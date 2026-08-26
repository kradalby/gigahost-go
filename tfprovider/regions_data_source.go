package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource              = (*regionDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*regionDataSource)(nil)
	_ datasource.DataSource              = (*regionsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*regionsDataSource)(nil)
)

// regionDataSource looks up one deploy region by slug or name.
type regionDataSource struct {
	client *gigahost.Client
}

type regionModel struct {
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	NameShort types.String `tfsdk:"name_short"`
	Country   types.String `tfsdk:"country"`
	Active    types.Bool   `tfsdk:"active"`
}

// NewRegionDataSource constructs the singular data source.
func NewRegionDataSource() datasource.DataSource { return &regionDataSource{} }

// Metadata sets the data source type name.
func (d *regionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_region"
}

// Schema returns the Terraform schema.
func (d *regionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up one deploy region by slug (`sfj`), name (`sandefjord`), " +
			"or short name (`SFJ, NO`). List regions with `gigahost deploy regions`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Region slug, name, or short name to look up. " +
					"Filled with the canonical region name.",
				Required: true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Region slug for `gigahost_server.region`.",
				Computed:            true,
			},
			"name_short": schema.StringAttribute{MarkdownDescription: "Short code, e.g. `SFJ, NO`.", Computed: true},
			"country":    schema.StringAttribute{MarkdownDescription: "Two-letter country code.", Computed: true},
			"active":     schema.BoolAttribute{MarkdownDescription: "True when the region accepts new deployments.", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *regionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*gigahost.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))

		return
	}

	d.client = client
}

// Read resolves the region against the live catalog.
func (d *regionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config regionModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cat, err := d.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	region, err := cat.FindRegion(config.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unknown region", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, regionToModel(*region))...)
}

func regionToModel(r gigahost.DeployRegion) regionModel {
	return regionModel{
		Name:      types.StringValue(r.Name),
		Slug:      types.StringValue(r.Slug()),
		NameShort: types.StringValue(r.NameShort),
		Country:   types.StringValue(r.Country),
		Active:    types.BoolValue(r.Active),
	}
}

// regionsDataSource lists all deploy regions.
type regionsDataSource struct {
	client *gigahost.Client
}

type regionsModel struct {
	Regions []regionModel `tfsdk:"regions"`
}

// NewRegionsDataSource constructs the plural data source.
func NewRegionsDataSource() datasource.DataSource { return &regionsDataSource{} }

// Metadata sets the data source type name.
func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}

// Schema returns the Terraform schema.
func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all deploy regions. Same data as `gigahost deploy regions`.",
		Attributes: map[string]schema.Attribute{
			"regions": schema.ListNestedAttribute{
				MarkdownDescription: "Every deploy region.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{MarkdownDescription: "Region name, e.g. `Sandefjord`.", Computed: true},
						"slug": schema.StringAttribute{
							MarkdownDescription: "Region slug for `gigahost_server.region`.",
							Computed:            true,
						},
						"name_short": schema.StringAttribute{MarkdownDescription: "Short code, e.g. `SFJ, NO`.", Computed: true},
						"country":    schema.StringAttribute{MarkdownDescription: "Two-letter country code.", Computed: true},
						"active":     schema.BoolAttribute{MarkdownDescription: "True when the region accepts new deployments.", Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *regionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*gigahost.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))

		return
	}

	d.client = client
}

// Read lists the regions from the live catalog.
func (d *regionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	cat, err := d.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	out := regionsModel{Regions: make([]regionModel, 0, len(cat.Regions))}

	for _, r := range cat.Regions {
		out.Regions = append(out.Regions, regionToModel(r))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
