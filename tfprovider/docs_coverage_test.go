package tfprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Repo layout, relative to this package (tfprovider/). The registry docs and
// examples live in the nested shim module; the project README lists coverage.
const (
	shimDir    = "../terraform-provider-gigahost"
	readmePath = "../README.md"
)

// registeredTypes returns the gigahost_* type names the provider registers,
// split into resources and data sources, by asking each one for its Metadata.
func registeredTypes(t *testing.T) ([]string, []string) {
	t.Helper()

	ctx := context.Background()
	p := New("test")()

	var meta provider.MetadataResponse
	p.Metadata(ctx, provider.MetadataRequest{}, &meta)
	prov := meta.TypeName // "gigahost"

	var resources, dataSources []string

	for _, newR := range p.Resources(ctx) {
		var resp resource.MetadataResponse
		newR().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: prov}, &resp)
		resources = append(resources, resp.TypeName)
	}

	for _, newD := range p.DataSources(ctx) {
		var resp datasource.MetadataResponse
		newD().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: prov}, &resp)
		dataSources = append(dataSources, resp.TypeName)
	}

	return resources, dataSources
}

// TestDocsCoverage is the consistency gate: every registered resource and data
// source must ship a generated registry doc, a runnable example, and a mention
// in the project README. It fails the moment a new one is added without them,
// which is how the README and docs are kept in sync over time.
func TestDocsCoverage(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(shimDir); err != nil {
		t.Skipf("registry shim not present (%v); coverage check is repo-only", err)
	}

	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}

	readmeText := string(readme)

	resources, dataSources := registeredTypes(t)

	check := func(kind, dir string, names []string) {
		for _, name := range names {
			short := strings.TrimPrefix(name, "gigahost_")

			doc := filepath.Join(shimDir, "docs", dir, short+".md")
			if _, err := os.Stat(doc); err != nil {
				t.Errorf("%s %q: missing generated doc %s (run 'nix run .#tfdocs')", kind, name, doc)
			}

			example := filepath.Join(shimDir, "examples", dir, name)
			if _, err := os.Stat(example); err != nil {
				t.Errorf("%s %q: missing example dir %s", kind, name, example)
			}

			if !strings.Contains(readmeText, name) {
				t.Errorf("%s %q: not mentioned in README coverage", kind, name)
			}
		}
	}

	check("resource", "resources", resources)
	check("data source", "data-sources", dataSources)
}
