package client

import (
	"os"
	"strings"
	"testing"
)

// loadCatalogFixture decodes testdata/deploy/catalog.json (the live payload
// shape) straight into a DeployCatalog.
func loadCatalogFixture(t *testing.T) *DeployCatalog {
	t.Helper()

	raw, err := os.ReadFile("testdata/deploy/catalog.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var env struct {
		Data DeployCatalog `json:"data"`
	}

	if err := unmarshalJSON(raw, &env); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return &env.Data
}

func TestParseSizeSlug(t *testing.T) {
	t.Parallel()

	valid := map[string]SizeSpec{
		"2c-4gb-40gb":      {Cores: 2, MemoryGB: 4, StorageGB: 40},
		"16c-32gb-160gb":   {Cores: 16, MemoryGB: 32, StorageGB: 160},
		"2C-4GB-40GB":      {Cores: 2, MemoryGB: 4, StorageGB: 40},
		"  2c-4gb-40gb  ":  {Cores: 2, MemoryGB: 4, StorageGB: 40},
		"100c-512gb-999gb": {Cores: 100, MemoryGB: 512, StorageGB: 999},
	}

	for in, want := range valid {
		got, err := ParseSizeSlug(in)
		if err != nil {
			t.Errorf("ParseSizeSlug(%q): %v", in, err)

			continue
		}

		if got != want {
			t.Errorf("ParseSizeSlug(%q) = %+v, want %+v", in, got, want)
		}
	}

	invalid := []string{
		"",
		"2c-4gb",
		"4gb",
		"2c-4gb-40tb",
		"2x-4gb-40gb",
		"-2c-4gb-40gb",
		"2c-4gb-40gb-extra",
		"two-four-forty",
	}

	for _, in := range invalid {
		if _, err := ParseSizeSlug(in); err == nil {
			t.Errorf("ParseSizeSlug(%q): want error", in)
		} else if !strings.Contains(err.Error(), "{cores}c-{ram}gb-{disk}gb") {
			t.Errorf("ParseSizeSlug(%q) error %q does not show the grammar", in, err)
		}
	}
}

func FuzzParseSizeSlug(f *testing.F) {
	for _, seed := range []string{"2c-4gb-40gb", "", "c-gb-gb", "999999999999999999999c-1gb-1gb", "2C-4GB-40GB"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		spec, err := ParseSizeSlug(in)
		if err != nil {
			return
		}

		// A successful parse must round-trip through the canonical form.
		again, err := ParseSizeSlug(spec.String())
		if err != nil {
			t.Fatalf("canonical %q from %q does not re-parse: %v", spec.String(), in, err)
		}

		if again != spec {
			t.Fatalf("round-trip mismatch: %+v vs %+v", spec, again)
		}
	})
}

func TestSizeSlugDerivation(t *testing.T) {
	t.Parallel()

	p := DeployProduct{Specs: DeploySpecs{CPUCores: 2, RAMGB: 4, Disks: []DeployDisk{{SizeGB: 40, Type: "NVMe"}}}}
	if got := p.SizeSlug(); got != "2c-4gb-40gb" {
		t.Errorf("SizeSlug = %q, want 2c-4gb-40gb", got)
	}

	// Multi-disk products sum their storage.
	p.Specs.Disks = append(p.Specs.Disks, DeployDisk{SizeGB: 500, Type: "HDD"})
	if got := p.SizeSlug(); got != "2c-4gb-540gb" {
		t.Errorf("SizeSlug multi-disk = %q, want 2c-4gb-540gb", got)
	}

	// Metal (cpu_cores=0): CPU model leads the slug — the live catalog has
	// two 16GB/500GB intros differing only in CPU.
	metal := DeployProduct{Specs: DeploySpecs{
		CPUModel: "Intel Core i5-2400 3.1GHz",
		RAMGB:    16,
		Disks:    []DeployDisk{{SizeGB: 500, Type: "SSD"}},
	}}
	if got := metal.SizeSlug(); got != "core-i5-2400-16gb-500gb" {
		t.Errorf("metal SizeSlug = %q, want core-i5-2400-16gb-500gb", got)
	}

	metal.Specs.CPUModel = "AMD Ryzen 5 3600 3.6GHz"
	if got := metal.SizeSlug(); got != "ryzen-5-3600-16gb-500gb" {
		t.Errorf("metal SizeSlug = %q, want ryzen-5-3600-16gb-500gb", got)
	}

	// No cores, no CPU model: fall back to ram/disk.
	metal.Specs.CPUModel = ""
	if got := metal.SizeSlug(); got != "16gb-500gb" {
		t.Errorf("fallback SizeSlug = %q, want 16gb-500gb", got)
	}
}

func TestTierTypeSlug(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"KVM Value":          "value",
		"KVM Performance":    "performance",
		"Intro Bare Metal":   "intro",
		"Auction Bare Metal": "auction",
	}

	for in, want := range cases {
		tier := DeployTier{GroupName: in}
		if got := tier.TypeSlug(); got != want {
			t.Errorf("TypeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlatformSlug(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		ProductTypeVM:        PlatformCloud,
		ProductTypeDedicated: PlatformMetal,
		ProductTypeAuction:   PlatformMetal,
		"":                   "",
		"mystery":            "",
	}

	for in, want := range cases {
		product := DeployProduct{Type: in}
		if got := product.PlatformSlug(); got != want {
			t.Errorf("PlatformSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// liveOSes mirrors the OS list observed on the live API (2026-06)
// so slug derivation is pinned against real display names.
func liveOSes() []ResolvedOS {
	mk := func(id, name, codename string) ResolvedOS {
		os := ReinstallOS{ID: id, Name: name, Distribution: codename, Arch: "amd64"}

		return ResolvedOS{OS: os, Slug: OSSlug(os)}
	}

	return []ResolvedOS{
		mk("88", "Debian 11 64-bit", "bullseye"),
		mk("101", "Debian 12 64-bit", "bookworm"),
		mk("108", "Debian 13 64-bit", "trixie"),
		mk("107", "Proxmox VE 64-bit", "trixie"),
		mk("93", "Ubuntu 22.04 LTS", "jammy"),
		mk("102", "Ubuntu 24.04 LTS", "noble"),
		mk("116", "Ubuntu 26.04 LTS", "resolute"),
		mk("109", "Windows 11", "win11"),
		mk("110", "Windows Server 2025 Standard", "win2025"),
		mk("111", "Windows 10", "win10"),
		mk("83", "AlmaLinux 8 64-bit", "8"),
		mk("96", "AlmaLinux 9 64-bit", "9"),
		mk("117", "AlmaLinux 10 64-bit", "10"),
		mk("105", "Fedora 43 Server 64-bit", "43"),
		mk("92", "RockyLinux 8 64-bit", "8"),
		mk("95", "RockyLinux 9 64-bit", "9"),
	}
}

func TestOSSlugDerivation(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"Debian 12 64-bit":             "debian-12",
		"Ubuntu 24.04 LTS":             "ubuntu-24.04",
		"Windows Server 2025 Standard": "windows-server-2025",
		"Windows 11":                   "windows-11",
		"Proxmox VE 64-bit":            "proxmox-ve",
		"Fedora 43 Server 64-bit":      "fedora-43-server",
		"AlmaLinux 9 64-bit":           "almalinux-9",
		"RockyLinux 8 64-bit":          "rockylinux-8",
	}

	for name, slug := range want {
		if got := OSSlug(ReinstallOS{Name: name}); got != slug {
			t.Errorf("OSSlug(%q) = %q, want %q", name, got, slug)
		}
	}
}

func TestOSSlugUniqueness(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, o := range liveOSes() {
		if prev, dup := seen[o.Slug]; dup {
			t.Errorf("slug %q collides: %q and %q", o.Slug, prev, o.OS.Name)
		}

		seen[o.Slug] = o.OS.Name
	}
}

func TestResolveOSIn(t *testing.T) {
	t.Parallel()

	all := liveOSes()

	resolves := map[string]string{ // input -> want os_id
		"debian-12":           "101",
		"DEBIAN-12":           "101",
		"bookworm":            "101",
		"Debian 12 64-bit":    "101",
		"noble":               "102",
		"ubuntu-24.04":        "102",
		"windows-server-2025": "110",
		"win2025":             "110",
		"proxmox":             "107",
		"fedora":              "105",
		"101":                 "101", // raw ID passthrough
		"debian-13":           "108", // exact beats trixie codename collision with proxmox
	}

	for in, wantID := range resolves {
		got, err := resolveOSIn(all, in)
		if err != nil {
			t.Errorf("resolveOSIn(%q): %v", in, err)

			continue
		}

		if got.OS.ID != wantID {
			t.Errorf("resolveOSIn(%q) = os %s (%s), want %s", in, got.OS.ID, got.Slug, wantID)
		}
	}

	// Ambiguity: "debian-1" substring-matches debian-11/12/13.
	if _, err := resolveOSIn(all, "debian-1"); err == nil {
		t.Error("resolveOSIn(debian-1): want ambiguity error")
	} else {
		for _, want := range []string{"ambiguous", "debian-11", "debian-12", "debian-13"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("ambiguity error %q missing %q", err, want)
			}
		}
	}

	// Not found: error lists valid slugs.
	if _, err := resolveOSIn(all, "temple-os"); err == nil {
		t.Error("resolveOSIn(temple-os): want error")
	} else if !strings.Contains(err.Error(), "debian-12") || !strings.Contains(err.Error(), "ubuntu-24.04") {
		t.Errorf("not-found error %q does not list valid slugs", err)
	}

	// Unknown raw ID.
	if _, err := resolveOSIn(all, "99999"); err == nil {
		t.Error("resolveOSIn(99999): want error")
	}
}

func TestCatalogSlugRoundTrip(t *testing.T) {
	t.Parallel()

	cat := loadCatalogFixture(t)

	type key struct{ platform, typ, size string }

	seen := map[key]string{}

	for _, tier := range cat.Tiers {
		typeSlug := tier.TypeSlug()

		for _, p := range tier.Products {
			if !p.Deployable() {
				continue
			}

			slug := p.SizeSlug()

			if slug == "" {
				t.Errorf("product %q derived an empty slug", p.Name)

				continue
			}

			// Grammar: cloud slugs must parse the canonical triple; metal
			// slugs lead with the CPU model and are opaque strings.
			if p.PlatformSlug() == PlatformCloud {
				spec, err := ParseSizeSlug(slug)
				if err != nil {
					t.Errorf("derived slug %q (product %s) does not parse: %v", slug, p.Name, err)

					continue
				}

				if spec.String() != slug {
					t.Errorf("slug %q is not canonical (re-renders as %q)", slug, spec.String())
				}
			}

			// Uniqueness within (platform, type).
			k := key{p.PlatformSlug(), typeSlug, slug}
			if prev, dup := seen[k]; dup {
				t.Errorf("slug collision in %s/%s: %q held by %q and %q", k.platform, k.typ, slug, prev, p.Name)
			}

			seen[k] = p.Name

			// Bijectivity: selector built from the derived slugs finds
			// exactly this product.
			got, err := cat.FindProduct(ProductSelector{Platform: p.PlatformSlug(), Type: typeSlug, Size: slug})
			if err != nil {
				t.Errorf("FindProduct(%s/%s/%s): %v", p.PlatformSlug(), typeSlug, slug, err)

				continue
			}

			if got.ID != p.ID {
				t.Errorf("FindProduct(%s/%s/%s) = product %s, want %s", p.PlatformSlug(), typeSlug, slug, got.ID, p.ID)
			}
		}
	}
}

func TestFindProduct(t *testing.T) {
	t.Parallel()

	cat := loadCatalogFixture(t)

	found := []struct {
		desc string
		sel  ProductSelector
		want string // product name
	}{
		{"type+size exact", ProductSelector{Type: "value", Size: "2c-4gb-40gb"}, "KVM Value VPS 4GB"},
		{"lenient size substring", ProductSelector{Type: "value", Size: "4gb-40"}, "KVM Value VPS 4GB"},
		{"cheapest tiebreak across types", ProductSelector{Size: "2c-4gb-40gb", Cheapest: true}, "KVM Value VPS 4GB"},
		{"cheapest cloud overall", ProductSelector{Cheapest: true}, "KVM Value VPS 4GB"},
		{"memory criteria + type", ProductSelector{Type: "value", MemoryGB: 8}, "KVM Value VPS 8GB"},
		{"cores criteria + cheapest", ProductSelector{Cores: 2, Cheapest: true}, "KVM Value VPS 4GB"},
		{"metal cheapest", ProductSelector{Platform: "metal", Cheapest: true}, "Intro - Intel Core i3 4GB"},
	}

	for _, tc := range found {
		p, err := cat.FindProduct(tc.sel)
		if err != nil {
			t.Errorf("%s: %v", tc.desc, err)

			continue
		}

		if p.Name != tc.want {
			t.Errorf("%s: got %q, want %q", tc.desc, p.Name, tc.want)
		}
	}

	failures := []struct {
		desc      string
		sel       ProductSelector
		wantInErr []string
	}{
		{
			"ambiguous size across types",
			ProductSelector{Size: "2c-4gb-40gb"},
			[]string{"value/2c-4gb-40gb", "performance/2c-4gb-40gb", "cheapest"},
		},
		{
			"unknown type lists valid types",
			ProductSelector{Type: "turbo"},
			[]string{`"turbo"`, "performance", "value"},
		},
		{
			"unknown size lists candidates with rates",
			ProductSelector{Type: "value", Size: "3c-4gb-40gb"},
			[]string{"value/2c-4gb-40gb (0.07826 NOK/h)"},
		},
		{
			"impossible criteria",
			ProductSelector{MemoryGB: 3},
			[]string{"memory_gb=3"},
		},
		{
			"unknown platform lists platforms",
			ProductSelector{Platform: "orbital"},
			[]string{"cloud", "metal"},
		},
	}

	for _, tc := range failures {
		_, err := cat.FindProduct(tc.sel)
		if err == nil {
			t.Errorf("%s: want error", tc.desc)

			continue
		}

		for _, want := range tc.wantInErr {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q missing %q", tc.desc, err, want)
			}
		}
	}

	// Auction products are never deployable, even via metal.
	for _, tier := range cat.Tiers {
		for _, prod := range tier.Products {
			if prod.Type == ProductTypeAuction && prod.Deployable() {
				t.Errorf("auction product %q reported deployable", prod.Name)
			}
		}
	}
}

func TestFindRegion(t *testing.T) {
	t.Parallel()

	cat := loadCatalogFixture(t)

	for _, in := range []string{"sfj", "SFJ", "SFJ, NO", "sandefjord", "Sandefjord", "1", "sande"} {
		r, err := cat.FindRegion(in)
		if err != nil {
			t.Errorf("FindRegion(%q): %v", in, err)

			continue
		}

		if r.ID != "1" {
			t.Errorf("FindRegion(%q) = region %s", in, r.ID)
		}
	}

	if _, err := cat.FindRegion("oslo"); err == nil {
		t.Error("FindRegion(oslo): want error")
	} else if !strings.Contains(err.Error(), "sandefjord (sfj)") {
		t.Errorf("region error %q does not list candidates", err)
	}

	if _, err := cat.FindRegion("99"); err == nil {
		t.Error("FindRegion(99): want error")
	}
}

func TestRegionForProduct(t *testing.T) {
	t.Parallel()

	cat := loadCatalogFixture(t)
	p := &cat.Tiers[0].Products[0] // region_ids [1]

	// Single offered region: empty input auto-resolves.
	r, err := cat.RegionForProduct(p, "")
	if err != nil {
		t.Fatalf("RegionForProduct(empty): %v", err)
	}

	if r.ID != "1" {
		t.Errorf("auto region = %s", r.ID)
	}

	// Explicit matching region works.
	if _, err := cat.RegionForProduct(p, "sfj"); err != nil {
		t.Errorf("RegionForProduct(sfj): %v", err)
	}

	// Multi-region product with empty input demands a choice.
	multi := *p
	multi.RegionIDs = []string{"1", "2"}
	twoRegion := *cat
	twoRegion.Regions = append(twoRegion.Regions, DeployRegion{ID: "2", Name: "Oslo", NameShort: "OSL, NO", Active: true})

	if _, err := twoRegion.RegionForProduct(&multi, ""); err == nil {
		t.Error("want pick-one error for multi-region product")
	} else if !strings.Contains(err.Error(), "oslo (osl)") || !strings.Contains(err.Error(), "sandefjord (sfj)") {
		t.Errorf("pick-one error %q does not list regions", err)
	}

	// Region the product is not offered in.
	if _, err := twoRegion.RegionForProduct(p, "oslo"); err == nil {
		t.Error("want not-offered error")
	} else if !strings.Contains(err.Error(), "not offered in region") {
		t.Errorf("not-offered error %q", err)
	}
}
