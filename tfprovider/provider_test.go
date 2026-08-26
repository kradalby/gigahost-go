package tfprovider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/kradalby/gigahost-go/tfprovider"
)

// TestProviderMetadata verifies the provider reports the expected
// type name and version.
func TestProviderMetadata(t *testing.T) {
	t.Parallel()

	p := tfprovider.New("1.2.3")()

	var resp provider.MetadataResponse
	p.Metadata(context.Background(), provider.MetadataRequest{}, &resp)

	if resp.TypeName != "gigahost" {
		t.Errorf("TypeName = %q, want gigahost", resp.TypeName)
	}

	if resp.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", resp.Version)
	}
}

// TestProviderSchema verifies the provider schema validates cleanly.
func TestProviderSchema(t *testing.T) {
	t.Parallel()

	p := tfprovider.New("test")()

	var resp provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("schema errors: %v", resp.Diagnostics)
	}

	for _, attr := range []string{"token", "username", "password", "base_url"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}

// resourceLister narrows the Provider interface for the resource
// schema sweep test below.
type resourceLister interface {
	Resources(ctx context.Context) []func() resource.Resource
}

// dataSourceLister narrows the Provider interface for the data-source
// schema sweep test below.
type dataSourceLister interface {
	DataSources(ctx context.Context) []func() datasource.DataSource
}

// TestResources verifies every registered resource returns a valid
// schema and metadata.
func TestResources(t *testing.T) {
	t.Parallel()

	p, ok := tfprovider.New("test")().(resourceLister)
	if !ok {
		t.Fatalf("provider does not implement resourceLister")
	}

	ctx := context.Background()

	for _, ctor := range p.Resources(ctx) {
		r := ctor()

		var mResp resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "gigahost"}, &mResp)

		if mResp.TypeName == "" {
			t.Errorf("resource has empty TypeName: %T", r)
		}

		var sResp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sResp)

		if sResp.Diagnostics.HasError() {
			t.Errorf("%s schema errors: %v", mResp.TypeName, sResp.Diagnostics)
		}
	}
}

// TestDataSources verifies every registered data source returns a valid
// schema and metadata.
func TestDataSources(t *testing.T) {
	t.Parallel()

	p, ok := tfprovider.New("test")().(dataSourceLister)
	if !ok {
		t.Fatalf("provider does not implement dataSourceLister")
	}

	ctx := context.Background()

	for _, ctor := range p.DataSources(ctx) {
		d := ctor()

		var mResp datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "gigahost"}, &mResp)

		if mResp.TypeName == "" {
			t.Errorf("data source has empty TypeName: %T", d)
		}

		var sResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &sResp)

		if sResp.Diagnostics.HasError() {
			t.Errorf("%s schema errors: %v", mResp.TypeName, sResp.Diagnostics)
		}
	}
}
