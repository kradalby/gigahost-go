//go:build e2e

package e2e

import (
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestSlugContractCatalog is the trip-wire for the slug scheme against the
// live catalog: every deployable product must yield a grammar-valid size
// slug that is unique within its (platform, type) and round-trips through
// FindProduct back to the same product. A failure here means gigahost
// changed the catalog shape in a way that breaks slug-based selection —
// the message prints the offending product.
func TestSlugContractCatalog(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	cat, err := c.Deploy.GetCatalog(ctx)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}

	type key struct{ platform, typ, size string }

	seen := map[key]string{}
	deployable := 0

	for _, tier := range cat.Tiers {
		typeSlug := tier.TypeSlug()

		for _, p := range tier.Products {
			if !p.Deployable() {
				continue
			}

			deployable++

			slug := p.SizeSlug()

			if slug == "" {
				t.Errorf("product %q (id %s, specs %+v) derived an empty slug", p.Name, p.ID, p.Specs)

				continue
			}

			// Cloud slugs must parse the canonical triple grammar; metal
			// slugs lead with the CPU model and are opaque.
			if p.PlatformSlug() == gigahost.PlatformCloud {
				spec, err := gigahost.ParseSizeSlug(slug)
				if err != nil {
					t.Errorf("product %q (id %s, specs %+v): derived slug %q does not parse: %v",
						p.Name, p.ID, p.Specs, slug, err)

					continue
				}

				if spec.String() != slug {
					t.Errorf("product %q: slug %q not canonical (re-renders %q)", p.Name, slug, spec.String())
				}
			}

			k := key{p.PlatformSlug(), typeSlug, slug}
			if prev, dup := seen[k]; dup {
				t.Errorf("slug collision in %s/%s: %q held by %q and %q — scheme needs a disambiguator",
					k.platform, k.typ, slug, prev, p.Name)
			}

			seen[k] = p.Name

			got, err := cat.FindProduct(gigahost.ProductSelector{
				Platform: p.PlatformSlug(),
				Type:     typeSlug,
				Size:     slug,
			})
			if err != nil {
				t.Errorf("FindProduct(%s/%s/%s): %v", k.platform, typeSlug, slug, err)

				continue
			}

			if got.ID != p.ID {
				t.Errorf("FindProduct(%s/%s/%s) = product %s, want %s", k.platform, typeSlug, slug, got.ID, p.ID)
			}
		}
	}

	if deployable == 0 {
		t.Fatal("catalog has no deployable products — contract test cannot run")
	}

	t.Logf("verified %d deployable products", deployable)

	// Regions: every active region resolvable by name and short code.
	for _, r := range cat.Regions {
		for _, in := range []string{r.Name, r.NameShort} {
			got, err := cat.FindRegion(in)
			if err != nil {
				t.Errorf("FindRegion(%q): %v", in, err)

				continue
			}

			if got.ID != r.ID {
				t.Errorf("FindRegion(%q) = region %s, want %s", in, got.ID, r.ID)
			}
		}
	}
}

// TestSlugContractOperatingSystems verifies every live OS yields a unique,
// resolvable slug.
func TestSlugContractOperatingSystems(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	all, err := c.Reinstall.ListAllOperatingSystems(ctx)
	if err != nil {
		t.Fatalf("ListAllOperatingSystems: %v", err)
	}

	if len(all) == 0 {
		t.Fatal("no operating systems returned")
	}

	seen := map[string]string{}

	for _, o := range all {
		if o.Slug == "" {
			t.Errorf("os %q (id %s): empty slug", o.OS.Name, o.OS.ID)

			continue
		}

		if prev, dup := seen[o.Slug]; dup {
			t.Errorf("os slug collision: %q held by %q and %q", o.Slug, prev, o.OS.Name)
		}

		seen[o.Slug] = o.OS.Name

		got, err := c.Reinstall.ResolveOS(ctx, o.Slug)
		if err != nil {
			t.Errorf("ResolveOS(%q): %v", o.Slug, err)

			continue
		}

		if got.OS.ID != o.OS.ID {
			t.Errorf("ResolveOS(%q) = os %s (%q), want %s (%q)", o.Slug, got.OS.ID, got.OS.Name, o.OS.ID, o.OS.Name)
		}
	}

	t.Logf("verified %d operating systems", len(all))
}
