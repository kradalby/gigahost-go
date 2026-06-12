package tfprovider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// Ensure gigahostProvider satisfies the provider interface.
var _ provider.Provider = (*gigahostProvider)(nil)

// gigahostProvider is the Terraform Plugin Framework provider
// implementation for Gigahost. It is created by [New].
type gigahostProvider struct {
	version string
}

// gigahostProviderModel is the Terraform schema model for the
// provider block.
type gigahostProviderModel struct {
	Token    types.String `tfsdk:"token"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	BaseURL  types.String `tfsdk:"base_url"`
}

// Metadata returns the provider type name and version.
func (p *gigahostProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gigahost"
	resp.Version = p.version
}

// Schema returns the provider schema.
func (p *gigahostProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage resources on gigahost.no, a Norwegian hosting provider.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				MarkdownDescription: "API bearer token. May also be supplied via the " +
					"`GIGAHOST_TOKEN` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Account email. May also be supplied via the " +
					"`GIGAHOST_USERNAME` environment variable.",
				Optional: true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Account password. May also be supplied via the " +
					"`GIGAHOST_PASSWORD` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Override the API base URL (defaults to " +
					"https://api.gigahost.no/api/v0).",
				Optional: true,
			},
		},
	}
}

// Configure reads the provider block, falls back to environment
// variables and constructs a [*gigahost.Client] used by all resources
// and data sources.
func (p *gigahostProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data gigahostProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	token := firstNonEmpty(data.Token.ValueString(), os.Getenv("GIGAHOST_TOKEN"))
	username := firstNonEmpty(data.Username.ValueString(), os.Getenv("GIGAHOST_USERNAME"))
	password := firstNonEmpty(data.Password.ValueString(), os.Getenv("GIGAHOST_PASSWORD"))
	baseURL := firstNonEmpty(data.BaseURL.ValueString(), os.Getenv("GIGAHOST_BASE_URL"))

	if token == "" && (username == "" || password == "") {
		resp.Diagnostics.AddError(
			"Missing Gigahost credentials",
			"Provide either a token or a username+password pair via the provider "+
				"block or the GIGAHOST_TOKEN / GIGAHOST_USERNAME / GIGAHOST_PASSWORD "+
				"environment variables.",
		)

		return
	}

	opts := []gigahost.Option{}

	if baseURL != "" {
		opts = append(opts, gigahost.WithBaseURL(baseURL))
	}

	if token != "" {
		opts = append(opts, gigahost.WithToken(token))
	} else {
		opts = append(opts, gigahost.WithCredentials(username, password, 0))
	}

	client, err := gigahost.NewClient(opts...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to construct Gigahost client", err.Error())

		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

// Resources returns the list of resource constructors.
func (p *gigahostProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDNSZoneResource,
		NewDNSRecordResource,
		NewDNSRedirectResource,
		NewDNSDNSSECResource,
		NewDNSPTRZoneResource,
		NewDNSNameserversResource,
		NewDNSExternalDSRecordsResource,
		NewBGPASNResource,
		NewBGPSessionResource,
		NewServerResource,
		NewServerIPv4Resource,
		NewServerSnapshotResource,
		NewServerNameResource,
		NewServerRDNSResource,
		NewAccountSSHKeyResource,
		NewAccountAPIKeyResource,
	}
}

// DataSources returns the list of data source constructors.
func (p *gigahostProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDNSZoneDataSource,
		NewDNSZonesDataSource,
		NewDNSRecordsDataSource,
		NewServerDataSource,
		NewServersDataSource,
		NewServerSizeDataSource,
		NewServerSizesDataSource,
		NewServerCatalogDataSource,
		NewRegionDataSource,
		NewRegionsDataSource,
		NewAccountDataSource,
		NewBGPDataSource,
		NewOperatingSystemDataSource,
		NewOperatingSystemsDataSource,
		NewISOsDataSource,
		NewSSHKeyDataSource,
		NewSSHKeysDataSource,
	}
}

// New is the constructor invoked by the thin provider binary.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &gigahostProvider{version: version}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}
