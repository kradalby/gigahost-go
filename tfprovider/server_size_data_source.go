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
	_ datasource.DataSource              = (*serverSizeDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*serverSizeDataSource)(nil)
)

// serverSizeDataSource selects exactly one deployable server size from the
// live catalog by slug and/or hardware criteria.
type serverSizeDataSource struct {
	client *gigahost.Client
}

type serverSizeModel struct {
	// Selection criteria; the slug/spec attributes are filled with the
	// resolved values, so they double as outputs.
	Platform  types.String `tfsdk:"platform"`
	Type      types.String `tfsdk:"type"`
	Size      types.String `tfsdk:"size"`
	Cores     types.Int64  `tfsdk:"cores"`
	MemoryGB  types.Int64  `tfsdk:"memory_gb"`
	StorageGB types.Int64  `tfsdk:"storage_gb"`
	Cheapest  types.Bool   `tfsdk:"cheapest"`

	// Resolved size.
	Slug        types.String  `tfsdk:"slug"`
	Category    types.String  `tfsdk:"category"`
	Name        types.String  `tfsdk:"name"`
	BandwidthGB types.Int64   `tfsdk:"bandwidth_gb"`
	RateHourly  types.Float64 `tfsdk:"rate_hourly"`
	RateMonthly types.Float64 `tfsdk:"rate_monthly"`
	Currency    types.String  `tfsdk:"currency"`
	Regions     types.List    `tfsdk:"regions"`
}

// NewServerSizeDataSource constructs the data source.
func NewServerSizeDataSource() datasource.DataSource { return &serverSizeDataSource{} }

// Metadata sets the data source type name.
func (d *serverSizeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_size"
}

// Schema returns the Terraform schema.
func (d *serverSizeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Selects exactly one deployable server size from the live catalog. " +
			"Give any combination of `type`, `size`, `cores`, `memory_gb`, `storage_gb`; if more " +
			"than one size matches, set `cheapest = true` to pick the lowest hourly rate, otherwise " +
			"the lookup errors listing the candidates. Feed the result into `gigahost_server` via " +
			"`type` and `slug`. List sizes with `gigahost deploy sizes`.",
		Attributes: map[string]schema.Attribute{
			"platform": schema.StringAttribute{
				MarkdownDescription: "Platform to search: `cloud` (default) or `metal`. " +
					"Filled with the resolved platform.",
				Optional: true,
				Computed: true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Server type slug, e.g. `value` or `performance` " +
					"(list with `gigahost deploy types`). Filled with the resolved type.",
				Optional: true,
				Computed: true,
			},
			"size": schema.StringAttribute{
				MarkdownDescription: "Size slug or unique fragment of one, e.g. `2c-4gb-40gb` or `4gb`.",
				Optional:            true,
			},
			"cores": schema.Int64Attribute{
				MarkdownDescription: "CPU core count to match. Filled with the resolved cores.",
				Optional:            true,
				Computed:            true,
			},
			"memory_gb": schema.Int64Attribute{
				MarkdownDescription: "Memory in GB to match. Filled with the resolved memory.",
				Optional:            true,
				Computed:            true,
			},
			"storage_gb": schema.Int64Attribute{
				MarkdownDescription: "Total storage in GB to match. Filled with the resolved storage.",
				Optional:            true,
				Computed:            true,
			},
			"cheapest": schema.BoolAttribute{
				MarkdownDescription: "Pick the lowest hourly rate when several sizes match.",
				Optional:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Canonical size slug for `gigahost_server.size`.",
				Computed:            true,
			},
			"category": schema.StringAttribute{
				MarkdownDescription: "Product category: `vm`, `dedicated`, or `auction`.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Commercial product name, e.g. `KVM Value VPS 4GB`.",
				Computed:            true,
			},
			"bandwidth_gb": schema.Int64Attribute{Computed: true},
			"rate_hourly":  schema.Float64Attribute{Computed: true},
			"rate_monthly": schema.Float64Attribute{Computed: true},
			"currency":     schema.StringAttribute{Computed: true},
			"regions": schema.ListAttribute{
				MarkdownDescription: "Region slugs the size is offered in.",
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *serverSizeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read resolves the selector against the live catalog.
func (d *serverSizeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config serverSizeModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cat, err := d.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	product, err := cat.FindProduct(gigahost.ProductSelector{
		Platform:  config.Platform.ValueString(),
		Type:      config.Type.ValueString(),
		Size:      config.Size.ValueString(),
		Cores:     int(config.Cores.ValueInt64()),
		MemoryGB:  int(config.MemoryGB.ValueInt64()),
		StorageGB: int(config.StorageGB.ValueInt64()),
		Cheapest:  config.Cheapest.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("No matching server size", err.Error())

		return
	}

	regions, diags := types.ListValueFrom(ctx, types.StringType, productRegionSlugs(cat, product))
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	spec, err := gigahost.ParseSizeSlug(product.SizeSlug())
	if err != nil {
		// Metal slugs are not the numeric triple; fall back to specs.
		spec = gigahost.SizeSpec{Cores: product.Specs.CPUCores, MemoryGB: product.Specs.RAMGB}
		for _, disk := range product.Specs.Disks {
			spec.StorageGB += disk.SizeGB
		}
	}

	config.Platform = types.StringValue(product.PlatformSlug())
	config.Type = types.StringValue(catalogTypeSlug(cat, product))
	config.Cores = types.Int64Value(int64(spec.Cores))
	config.MemoryGB = types.Int64Value(int64(spec.MemoryGB))
	config.StorageGB = types.Int64Value(int64(spec.StorageGB))
	config.Slug = types.StringValue(product.SizeSlug())
	config.Category = types.StringValue(product.Type)
	config.Name = types.StringValue(product.Name)
	config.BandwidthGB = types.Int64Value(int64(product.BandwidthGB))
	config.RateHourly = types.Float64Value(product.RateHourly)
	config.RateMonthly = types.Float64Value(product.RateMonthly)
	config.Currency = types.StringValue(cat.Currency)
	config.Regions = regions

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}

// productRegionSlugs maps a product's region IDs to region slugs.
func productRegionSlugs(cat *gigahost.DeployCatalog, p *gigahost.DeployProduct) []string {
	out := []string{}

	for _, id := range p.RegionIDs {
		for i := range cat.Regions {
			if cat.Regions[i].ID == id {
				out = append(out, cat.Regions[i].Slug())
			}
		}
	}

	return out
}

// catalogTypeSlug finds the type slug of the tier containing the product.
func catalogTypeSlug(cat *gigahost.DeployCatalog, p *gigahost.DeployProduct) string {
	for i := range cat.Tiers {
		for j := range cat.Tiers[i].Products {
			if cat.Tiers[i].Products[j].ID == p.ID && cat.Tiers[i].Products[j].PriceID == p.PriceID {
				return cat.Tiers[i].TypeSlug()
			}
		}
	}

	return ""
}
