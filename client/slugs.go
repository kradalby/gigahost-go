package client

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This file is the single source of truth for the human-readable slugs the
// CLI and Terraform provider expose instead of internal catalog IDs. Slugs
// are always DERIVED from live API data — nothing in here hardcodes catalog
// values. The schemes:
//
//	platform: "cloud" (API product type "vm") or "metal" ("dedicated")
//	type:     tier group_name minus platform noise — "value", "performance"
//	size:     "{cores}c-{ram}gb-{disk}gb" from the product's specs
//	region:   region name ("sandefjord") or short code ("sfj")
//	os:       slugified OS name — "debian-12", "ubuntu-24.04",
//	          "windows-server-2025", "proxmox-ve"
//
// Every Resolve*/Find* helper matches leniently (exact first, then unique
// substring, case-insensitive) and returns errors that list the valid
// values, so a wrong guess teaches the right one.

// Platform slugs derived from the catalog's product type.
const (
	PlatformCloud = "cloud"
	PlatformMetal = "metal"
)

// SizeSpec is the parsed form of a size slug like "2c-4gb-40gb".
type SizeSpec struct {
	Cores     int
	MemoryGB  int
	StorageGB int
}

// String renders the canonical slug form.
func (s SizeSpec) String() string {
	return strconv.Itoa(s.Cores) + "c-" + strconv.Itoa(s.MemoryGB) + "gb-" + strconv.Itoa(s.StorageGB) + "gb"
}

var sizeSlugRE = regexp.MustCompile(`^(\d+)c-(\d+)gb-(\d+)gb$`)

// ParseSizeSlug parses "2c-4gb-40gb" into its cores/memory/storage parts.
// The grammar is `{cores}c-{ram}gb-{disk}gb`, all base-10 integers.
func ParseSizeSlug(slug string) (SizeSpec, error) {
	m := sizeSlugRE.FindStringSubmatch(strings.ToLower(strings.TrimSpace(slug)))
	if m == nil {
		return SizeSpec{}, fmt.Errorf(
			"gigahost: invalid size %q: want {cores}c-{ram}gb-{disk}gb, e.g. 2c-4gb-40gb", slug,
		)
	}

	cores, err := strconv.Atoi(m[1])
	if err != nil {
		return SizeSpec{}, fmt.Errorf("gigahost: invalid size %q: cores %q: %w", slug, m[1], err)
	}

	mem, err := strconv.Atoi(m[2])
	if err != nil {
		return SizeSpec{}, fmt.Errorf("gigahost: invalid size %q: memory %q: %w", slug, m[2], err)
	}

	disk, err := strconv.Atoi(m[3])
	if err != nil {
		return SizeSpec{}, fmt.Errorf("gigahost: invalid size %q: storage %q: %w", slug, m[3], err)
	}

	return SizeSpec{Cores: cores, MemoryGB: mem, StorageGB: disk}, nil
}

// PlatformSlug maps the API product type to the user-facing platform slug.
// Auction products map to "metal" but are filtered out of deployable sets
// separately (they cannot be ordered hourly).
func (p *DeployProduct) PlatformSlug() string {
	switch p.Type {
	case ProductTypeVM:
		return PlatformCloud
	case ProductTypeDedicated, ProductTypeAuction:
		return PlatformMetal
	default:
		return ""
	}
}

// SizeSlug derives the canonical size slug from the product's hardware
// specs. Storage is the sum of all disks.
//
// Cloud products use the {cores}c-{ram}gb-{disk}gb triple. Metal products
// report cpu_cores=0 and are distinguished by CPU model instead (the live
// catalog has two 16GB/500GB intros differing only in i5 vs i7), so their
// slug leads with the CPU model: "core-i5-2400-16gb-500gb".
func (p *DeployProduct) SizeSlug() string {
	disk := 0
	for _, d := range p.Specs.Disks {
		disk += d.SizeGB
	}

	if p.Specs.CPUCores > 0 {
		return SizeSpec{Cores: p.Specs.CPUCores, MemoryGB: p.Specs.RAMGB, StorageGB: disk}.String()
	}

	ramDisk := strconv.Itoa(p.Specs.RAMGB) + "gb-" + strconv.Itoa(disk) + "gb"
	if cpu := cpuModelSlug(p.Specs.CPUModel); cpu != "" {
		return cpu + "-" + ramDisk
	}

	return ramDisk
}

// cpuNoiseTokens are dropped when slugifying a CPU model name: the vendor
// is implied and clock speed is not a selector.
var cpuNoiseTokens = map[string]bool{
	"intel": true,
	"amd":   true,
}

// cpuModelSlug slugifies a CPU model: "Intel Core i5-2400 3.1GHz" ->
// "core-i5-2400", "AMD Ryzen 5 3600 3.6GHz" -> "ryzen-5-3600".
func cpuModelSlug(model string) string {
	var out []string

	for tok := range strings.FieldsSeq(strings.ToLower(model)) {
		if cpuNoiseTokens[tok] || strings.HasSuffix(tok, "ghz") {
			continue
		}

		out = append(out, tok)
	}

	return strings.Join(out, "-")
}

// Deployable reports whether the product can be ordered through
// POST /deploy/servers: auction products and products without a price are
// catalog-visible but not hourly-deployable.
func (p *DeployProduct) Deployable() bool {
	return p.Type != ProductTypeAuction && p.PriceID != "" && p.PriceID != "0"
}

// tierNoiseTokens are dropped when deriving a tier type slug from
// group_name: they restate the platform, which is its own selector.
var tierNoiseTokens = map[string]bool{
	"kvm":   true,
	"vps":   true,
	"bare":  true,
	"metal": true,
}

// TypeSlug derives the user-facing type slug from the tier's group name:
// "KVM Value" -> "value", "KVM Performance" -> "performance",
// "Intro Bare Metal" -> "intro".
func (t *DeployTier) TypeSlug() string {
	return slugifyTokens(t.GroupName, tierNoiseTokens)
}

// osNoiseTokens are dropped when deriving an OS slug from os_name: they
// are marketing/arch suffixes that carry no selective power today.
var osNoiseTokens = map[string]bool{
	"64-bit":   true,
	"32-bit":   true,
	"lts":      true,
	"standard": true,
}

// OSSlug derives the canonical OS slug from the OS display name:
// "Debian 12 64-bit" -> "debian-12", "Ubuntu 24.04 LTS" -> "ubuntu-24.04",
// "Windows Server 2025 Standard" -> "windows-server-2025",
// "Proxmox VE 64-bit" -> "proxmox-ve".
func OSSlug(os ReinstallOS) string {
	return slugifyTokens(os.Name, osNoiseTokens)
}

// slugifyTokens lowercases, splits on whitespace, drops noise tokens, and
// joins with dashes.
func slugifyTokens(name string, noise map[string]bool) string {
	var out []string

	for tok := range strings.FieldsSeq(strings.ToLower(name)) {
		if noise[tok] {
			continue
		}

		out = append(out, tok)
	}

	return strings.Join(out, "-")
}

// matchLenient returns the indexes of candidates matching input: exact
// match (case-insensitive) wins outright; otherwise every candidate
// containing input as a substring matches.
func matchLenient(input string, candidates []string) []int {
	in := strings.ToLower(strings.TrimSpace(input))

	var exact, subs []int

	for i, c := range candidates {
		lc := strings.ToLower(c)
		if lc == in {
			exact = append(exact, i)
		} else if strings.Contains(lc, in) {
			subs = append(subs, i)
		}
	}

	if len(exact) > 0 {
		return exact
	}

	return subs
}

// allDigits reports whether s is a non-empty base-10 number — the
// convention for "this is a raw ID, skip name resolution".
func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// ProductSelector selects a deployable product by slugs and/or hardware
// criteria. Platform defaults to "cloud". Type and Size are matched
// leniently; Cores/MemoryGB/StorageGB (0 = unset) filter on the product's
// specs. When more than one product remains, Cheapest picks the lowest
// hourly rate instead of erroring.
type ProductSelector struct {
	Platform  string
	Type      string
	Size      string
	Cores     int
	MemoryGB  int
	StorageGB int
	Cheapest  bool
}

// totalDiskGB sums the product's disk sizes.
func (p *DeployProduct) totalDiskGB() int {
	disk := 0
	for _, d := range p.Specs.Disks {
		disk += d.SizeGB
	}

	return disk
}

// Slug returns the region's short slug: the lowercased airport-style code
// from region_name_short ("SFJ, NO" -> "sfj"), falling back to the
// lowercased name.
func (r *DeployRegion) Slug() string {
	if code, _, ok := strings.Cut(r.NameShort, ","); ok {
		if s := strings.ToLower(strings.TrimSpace(code)); s != "" {
			return s
		}
	}

	return strings.ToLower(r.Name)
}

// catalogEntry pairs a product with its derived slugs.
type catalogEntry struct {
	product  *DeployProduct
	typeSlug string
}

// describe renders "type/size (rate currency/h)" for error candidate lists.
func (e catalogEntry) describe(currency string) string {
	return fmt.Sprintf("%s/%s (%g %s/h)", e.typeSlug, e.product.SizeSlug(), e.product.RateHourly, currency)
}

// deployableEntries flattens the catalog into deployable products with
// derived slugs.
func (c *DeployCatalog) deployableEntries() []catalogEntry {
	var out []catalogEntry

	for i := range c.Tiers {
		typeSlug := c.Tiers[i].TypeSlug()

		for j := range c.Tiers[i].Products {
			p := &c.Tiers[i].Products[j]
			if !p.Deployable() {
				continue
			}

			out = append(out, catalogEntry{product: p, typeSlug: typeSlug})
		}
	}

	return out
}

// FindProduct resolves a ProductSelector against the catalog. Errors list
// the valid values so callers can surface them directly to users.
func (c *DeployCatalog) FindProduct(sel ProductSelector) (*DeployProduct, error) {
	entries := c.deployableEntries()
	if len(entries) == 0 {
		return nil, errors.New("gigahost: catalog has no deployable products")
	}

	platform := sel.Platform
	if platform == "" {
		platform = PlatformCloud
	}

	platform = strings.ToLower(strings.TrimSpace(platform))

	var pool []catalogEntry

	for _, e := range entries {
		if e.product.PlatformSlug() == platform {
			pool = append(pool, e)
		}
	}

	if len(pool) == 0 {
		return nil, fmt.Errorf("gigahost: no deployable products for platform %q; valid platforms: %s",
			sel.Platform, strings.Join(platformSlugs(entries), ", "))
	}

	if sel.Type != "" {
		types := make([]string, len(pool))
		for i, e := range pool {
			types[i] = e.typeSlug
		}

		idx := matchLenient(sel.Type, types)
		if len(idx) == 0 {
			return nil, fmt.Errorf("gigahost: type %q not found; valid types: %s",
				sel.Type, strings.Join(uniqueSorted(types), ", "))
		}

		pool = pick(pool, idx)
	}

	if sel.Size != "" {
		sizes := make([]string, len(pool))
		for i, e := range pool {
			sizes[i] = e.product.SizeSlug()
		}

		idx := matchLenient(sel.Size, sizes)
		if len(idx) == 0 {
			return nil, fmt.Errorf("gigahost: size %q not found; valid sizes: %s",
				sel.Size, strings.Join(describeAll(pool, c.Currency), ", "))
		}

		pool = pick(pool, idx)
	}

	for _, f := range []struct {
		name string
		want int
		get  func(*DeployProduct) int
	}{
		{"cores", sel.Cores, func(p *DeployProduct) int { return p.Specs.CPUCores }},
		{"memory_gb", sel.MemoryGB, func(p *DeployProduct) int { return p.Specs.RAMGB }},
		{"storage_gb", sel.StorageGB, func(p *DeployProduct) int { return p.totalDiskGB() }},
	} {
		if f.want == 0 {
			continue
		}

		var idx []int

		for i, e := range pool {
			if f.get(e.product) == f.want {
				idx = append(idx, i)
			}
		}

		if len(idx) == 0 {
			return nil, fmt.Errorf("gigahost: no product with %s=%d; valid sizes: %s",
				f.name, f.want, strings.Join(describeAll(pool, c.Currency), ", "))
		}

		pool = pick(pool, idx)
	}

	if len(pool) == 1 {
		return pool[0].product, nil
	}

	if sel.Cheapest {
		best := pool[0]
		for _, e := range pool[1:] {
			if e.product.RateHourly < best.product.RateHourly {
				best = e
			}
		}

		return best.product, nil
	}

	return nil, fmt.Errorf("gigahost: selector matches %d products: %s; add type/size or set cheapest",
		len(pool), strings.Join(describeAll(pool, c.Currency), ", "))
}

func pick(pool []catalogEntry, idx []int) []catalogEntry {
	out := make([]catalogEntry, len(idx))
	for i, j := range idx {
		out[i] = pool[j]
	}

	return out
}

func describeAll(pool []catalogEntry, currency string) []string {
	out := make([]string, len(pool))
	for i, e := range pool {
		out[i] = e.describe(currency)
	}

	sort.Strings(out)

	return out
}

func platformSlugs(entries []catalogEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.product.PlatformSlug())
	}

	return uniqueSorted(out)
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}

	var out []string

	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}

		seen[s] = true

		out = append(out, s)
	}

	sort.Strings(out)

	return out
}

// regionKeys returns the matchable names for a region: full name
// ("Sandefjord"), short code first token ("SFJ"), and full short name
// ("SFJ, NO").
func regionKeys(r DeployRegion) []string {
	keys := []string{r.Name}

	if r.NameShort != "" {
		keys = append(keys, r.NameShort)
		if code, _, ok := strings.Cut(r.NameShort, ","); ok {
			keys = append(keys, strings.TrimSpace(code))
		}
	}

	return keys
}

// regionSlugList renders "name (code)" candidates for error messages.
func regionSlugList(regions []DeployRegion) string {
	out := make([]string, len(regions))
	for i, r := range regions {
		out[i] = strings.ToLower(r.Name)
		if code, _, ok := strings.Cut(r.NameShort, ","); ok {
			out[i] += " (" + strings.ToLower(strings.TrimSpace(code)) + ")"
		}
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}

// FindRegion resolves a region by name ("sandefjord"), short code ("sfj"),
// full short name ("SFJ, NO"), or raw numeric ID.
func (c *DeployCatalog) FindRegion(input string) (*DeployRegion, error) {
	if len(c.Regions) == 0 {
		return nil, errors.New("gigahost: catalog has no regions")
	}

	if allDigits(input) {
		for i := range c.Regions {
			if c.Regions[i].ID == input {
				return &c.Regions[i], nil
			}
		}

		return nil, fmt.Errorf("gigahost: region ID %q not found; valid regions: %s",
			input, regionSlugList(c.Regions))
	}

	var matched []int

	for i := range c.Regions {
		if len(matchLenient(input, regionKeys(c.Regions[i]))) > 0 {
			matched = append(matched, i)
		}
	}

	switch len(matched) {
	case 1:
		return &c.Regions[matched[0]], nil
	case 0:
		return nil, fmt.Errorf("gigahost: region %q not found; valid regions: %s",
			input, regionSlugList(c.Regions))
	default:
		ambiguous := make([]DeployRegion, len(matched))
		for i, j := range matched {
			ambiguous[i] = c.Regions[j]
		}

		return nil, fmt.Errorf("gigahost: region %q is ambiguous; matches: %s",
			input, regionSlugList(ambiguous))
	}
}

// RegionForProduct resolves the deploy region for a product. With empty
// input, the product's sole region is used; multiple regions demand an
// explicit choice. With input, the resolved region must be one the product
// is offered in.
func (c *DeployCatalog) RegionForProduct(p *DeployProduct, input string) (*DeployRegion, error) {
	offered := make([]DeployRegion, 0, len(p.RegionIDs))

	for _, id := range p.RegionIDs {
		for i := range c.Regions {
			if c.Regions[i].ID == id {
				offered = append(offered, c.Regions[i])
			}
		}
	}

	if len(offered) == 0 {
		return nil, fmt.Errorf("gigahost: product %q is not offered in any known region", p.Name)
	}

	if input == "" {
		if len(offered) == 1 {
			return &offered[0], nil
		}

		return nil, fmt.Errorf("gigahost: product %q is offered in %d regions, pick one: %s",
			p.Name, len(offered), regionSlugList(offered))
	}

	region, err := c.FindRegion(input)
	if err != nil {
		return nil, err
	}

	for i := range offered {
		if offered[i].ID == region.ID {
			return region, nil
		}
	}

	return nil, fmt.Errorf("gigahost: product %q is not offered in region %q; offered in: %s",
		p.Name, input, regionSlugList(offered))
}

// ResolvedOS is one installable OS with its parent distribution and
// derived slug.
type ResolvedOS struct {
	OS           ReinstallOS
	Distribution Distribution
	Slug         string
}

// ListAllOperatingSystems fans out over all distributions and returns
// every installable OS with its derived slug.
func (s *ReinstallService) ListAllOperatingSystems(ctx context.Context) ([]ResolvedOS, error) {
	return s.allOSes.get(func() ([]ResolvedOS, error) {
		dists, err := s.ListDistributions(ctx)
		if err != nil {
			return nil, err
		}

		var out []ResolvedOS

		for _, dist := range dists {
			oses, err := s.ListOperatingSystems(ctx, dist.ID)
			if err != nil {
				return nil, fmt.Errorf("gigahost: list OSes for distribution %s (%s): %w", dist.ID, dist.Name, err)
			}

			for _, os := range oses {
				out = append(out, ResolvedOS{OS: os, Distribution: dist, Slug: OSSlug(os)})
			}
		}

		return out, nil
	})
}

// ResolveOS resolves user input to one installable OS. Accepted forms:
// canonical slug ("debian-12"), codename ("bookworm"), full display name
// ("Debian 12 64-bit"), unique substring of any of those, or a raw
// numeric os_id.
func (s *ReinstallService) ResolveOS(ctx context.Context, input string) (*ResolvedOS, error) {
	all, err := s.ListAllOperatingSystems(ctx)
	if err != nil {
		return nil, err
	}

	return resolveOSIn(all, input)
}

// resolveOSIn is the pure matching core of ResolveOS, separated for
// fixture-driven tests.
func resolveOSIn(all []ResolvedOS, input string) (*ResolvedOS, error) {
	if len(all) == 0 {
		return nil, errors.New("gigahost: no operating systems available")
	}

	if allDigits(input) {
		for i := range all {
			if all[i].OS.ID == input {
				return &all[i], nil
			}
		}

		return nil, fmt.Errorf("gigahost: os ID %q not found; valid: %s", input, osSlugList(all))
	}

	var matched []int

	for i := range all {
		keys := []string{all[i].Slug, all[i].OS.Distribution, all[i].OS.Name}
		if len(matchLenient(input, keys)) > 0 {
			matched = append(matched, i)
		}
	}

	// Prefer exact slug matches over substring hits across OSes:
	// "debian-1" substring-matches three slugs, but "debian-12" must win
	// outright even though it is also a substring of nothing else.
	var exact []int

	in := strings.ToLower(strings.TrimSpace(input))

	for _, i := range matched {
		if all[i].Slug == in || strings.EqualFold(all[i].OS.Distribution, in) || strings.EqualFold(all[i].OS.Name, in) {
			exact = append(exact, i)
		}
	}

	if len(exact) > 0 {
		matched = exact
	}

	switch len(matched) {
	case 1:
		return &all[matched[0]], nil
	case 0:
		return nil, fmt.Errorf("gigahost: os %q not found; valid: %s", input, osSlugList(all))
	default:
		ambiguous := make([]ResolvedOS, len(matched))
		for i, j := range matched {
			ambiguous[i] = all[j]
		}

		return nil, fmt.Errorf("gigahost: os %q is ambiguous; matches: %s", input, osSlugList(ambiguous))
	}
}

func osSlugList(all []ResolvedOS) string {
	out := make([]string, len(all))
	for i, o := range all {
		out[i] = o.Slug
	}

	return strings.Join(uniqueSorted(out), ", ")
}

// Resolve resolves a server by raw numeric ID or by name/hostname
// (case-insensitive exact match). The hostname given at deploy time lands
// in srv_name on the live API (srv_hostname stays empty), so both fields
// are matched.
func (s *ServersService) Resolve(ctx context.Context, idOrHostname string) (*Server, error) {
	if idOrHostname == "" {
		return nil, errors.New("gigahost: Resolve: empty server reference")
	}

	if allDigits(idOrHostname) {
		return s.Get(ctx, idOrHostname)
	}

	servers, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var matched []int

	for i := range servers {
		if strings.EqualFold(servers[i].Hostname, idOrHostname) ||
			strings.EqualFold(servers[i].Name, idOrHostname) {
			matched = append(matched, i)
		}
	}

	switch len(matched) {
	case 1:
		return &servers[matched[0]], nil
	case 0:
		if len(servers) == 0 {
			return nil, fmt.Errorf("gigahost: server %q not found; the account has no servers", idOrHostname)
		}

		names := make([]string, 0, len(servers)*2)

		for i := range servers {
			if servers[i].Name != "" {
				names = append(names, servers[i].Name)
			}

			if servers[i].Hostname != "" {
				names = append(names, servers[i].Hostname)
			}
		}

		return nil, fmt.Errorf("gigahost: server %q not found; known servers: %s",
			idOrHostname, strings.Join(uniqueSorted(names), ", "))
	default:
		return nil, fmt.Errorf("gigahost: hostname %q matches %d servers; use the server ID",
			idOrHostname, len(matched))
	}
}

// ResolveZone resolves a DNS zone by raw numeric ID or by zone name
// (case-insensitive exact match).
func (s *DNSService) ResolveZone(ctx context.Context, idOrName string) (*Zone, error) {
	if idOrName == "" {
		return nil, errors.New("gigahost: ResolveZone: empty zone reference")
	}

	zones, err := s.ListZones(ctx)
	if err != nil {
		return nil, err
	}

	if allDigits(idOrName) {
		for i := range zones {
			if zones[i].ID == idOrName {
				return &zones[i], nil
			}
		}
	}

	var matched []int

	for i := range zones {
		if strings.EqualFold(zones[i].Name, idOrName) {
			matched = append(matched, i)
		}
	}

	switch len(matched) {
	case 1:
		return &zones[matched[0]], nil
	case 0:
		if len(zones) == 0 {
			return nil, fmt.Errorf("gigahost: zone %q not found; the account has no zones", idOrName)
		}

		names := make([]string, len(zones))
		for i := range zones {
			names[i] = zones[i].Name
		}

		return nil, fmt.Errorf("gigahost: zone %q not found; known zones: %s",
			idOrName, strings.Join(uniqueSorted(names), ", "))
	default:
		return nil, fmt.Errorf("gigahost: zone name %q matches %d zones; use the zone ID", idOrName, len(matched))
	}
}

// ResolveISO resolves an uploaded deploy ISO by ID or by name (lenient
// match).
func (s *DeployService) ResolveISO(ctx context.Context, idOrName string) (*DeployISO, error) {
	if idOrName == "" {
		return nil, errors.New("gigahost: ResolveISO: empty ISO reference")
	}

	isos, err := s.ListISOs(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(isos))

	for i := range isos {
		if isos[i].ID == idOrName {
			return &isos[i], nil
		}

		names[i] = isos[i].Name
	}

	idx := matchLenient(idOrName, names)

	switch len(idx) {
	case 1:
		return &isos[idx[0]], nil
	case 0:
		return nil, fmt.Errorf("gigahost: iso %q not found; available: %s",
			idOrName, strings.Join(uniqueSorted(names), ", "))
	default:
		matchedNames := make([]string, len(idx))
		for i, j := range idx {
			matchedNames[i] = names[j]
		}

		return nil, fmt.Errorf("gigahost: iso %q is ambiguous; matches: %s",
			idOrName, strings.Join(uniqueSorted(matchedNames), ", "))
	}
}

// ResolveSSHKeys resolves a mix of key names and raw numeric IDs to key
// IDs, validating every reference against the account's stored keys.
func (s *AccountService) ResolveSSHKeys(ctx context.Context, namesOrIDs []string) ([]string, error) {
	if len(namesOrIDs) == 0 {
		return nil, nil
	}

	account, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}

	keys := account.SSHKeys

	known := make([]string, len(keys))
	for i := range keys {
		known[i] = keys[i].Name
	}

	out := make([]string, 0, len(namesOrIDs))

	for _, ref := range namesOrIDs {
		id := ""

		if allDigits(ref) {
			for i := range keys {
				if keys[i].ID == ref {
					id = ref

					break
				}
			}
		} else {
			idx := matchLenient(ref, known)

			switch len(idx) {
			case 1:
				id = keys[idx[0]].ID
			case 0:
				// fall through to the not-found error below
			default:
				matchedNames := make([]string, len(idx))
				for i, j := range idx {
					matchedNames[i] = known[j]
				}

				return nil, fmt.Errorf("gigahost: ssh key %q is ambiguous; matches: %s",
					ref, strings.Join(uniqueSorted(matchedNames), ", "))
			}
		}

		if id == "" {
			return nil, fmt.Errorf("gigahost: ssh key %q not found; known keys: %s",
				ref, strings.Join(uniqueSorted(known), ", "))
		}

		out = append(out, id)
	}

	return out, nil
}
