package tfprovider

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// readmePath is relative to this package; registry docs and examples are
// gated in terraform-provider-gigahost, against the commit it pins.
const readmePath = "../README.md"

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

// TestReadmeCoverage fails the moment a resource or data source is added
// without a mention in the project README coverage list.
func TestReadmeCoverage(t *testing.T) {
	t.Parallel()

	readme, err := os.ReadFile(readmePath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("README not in the source tree (nix sandbox); coverage check is repo-only")
	}

	if err != nil {
		t.Fatalf("read README: %v", err)
	}

	readmeText := string(readme)

	resources, dataSources := registeredTypes(t)

	check := func(kind string, names []string) {
		for _, name := range names {
			if !strings.Contains(readmeText, name) {
				t.Errorf("%s %q: not mentioned in README coverage", kind, name)
			}
		}
	}

	check("resource", resources)
	check("data source", dataSources)
}
