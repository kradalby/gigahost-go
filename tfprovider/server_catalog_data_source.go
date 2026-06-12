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
	_ datasource.DataSource              = (*serverCatalogDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*serverCatalogDataSource)(nil)
)

// serverCatalogDataSource returns the full deploy catalog — every tier,
// product, and region — for callers that want the raw price/product IDs rather
// than the resolved single-size view of gigahost_server_size(s).
type serverCatalogDataSource struct {
	client *gigahost.Client
}

type catalogProductModel struct {
	ProductID   types.String  `tfsdk:"product_id"`
	PriceID     types.String  `tfsdk:"price_id"`
	Name        types.String  `tfsdk:"name"`
	Category    types.String  `tfsdk:"category"`
	Platform    types.String  `tfsdk:"platform"`
	Slug        types.String  `tfsdk:"slug"`
	Cores       types.Int64   `tfsdk:"cores"`
	MemoryGB    types.Int64   `tfsdk:"memory_gb"`
	StorageGB   types.Int64   `tfsdk:"storage_gb"`
	BandwidthGB types.Int64   `tfsdk:"bandwidth_gb"`
	RateHourly  types.Float64 `tfsdk:"rate_hourly"`
	RateMonthly types.Float64 `tfsdk:"rate_monthly"`
	RegionIDs   types.List    `tfsdk:"region_ids"`
}

type catalogTierModel struct {
	GroupID   types.String          `tfsdk:"group_id"`
	GroupName types.String          `tfsdk:"group_name"`
	Products  []catalogProductModel `tfsdk:"products"`
}

type catalogRegionModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	NameShort types.String `tfsdk:"name_short"`
	Country   types.String `tfsdk:"country"`
	Active    types.Bool   `tfsdk:"active"`
}

type serverCatalogDataSourceModel struct {
	Currency types.String         `tfsdk:"currency"`
	Tiers    []catalogTierModel   `tfsdk:"tiers"`
	Regions  []catalogRegionModel `tfsdk:"regions"`
}

// NewServerCatalogDataSource constructs the data source.
func NewServerCatalogDataSource() datasource.DataSource { return &serverCatalogDataSource{} }

// Metadata sets the data source type name.
func (d *serverCatalogDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_catalog"
}

// Schema returns the Terraform schema.
func (d *serverCatalogDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns the full deploy catalog: every product tier, product, and " +
			"region, including raw `product_id`/`price_id`. Prefer `gigahost_server_size` for " +
			"selecting a single size by slug; use this to enumerate the whole catalog.",
		Attributes: map[string]schema.Attribute{
			"currency": schema.StringAttribute{Computed: true},
			"regions": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"name_short": schema.StringAttribute{Computed: true},
						"country":    schema.StringAttribute{Computed: true},
						"active":     schema.BoolAttribute{Computed: true},
					},
				},
			},
			"tiers": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group_id":   schema.StringAttribute{Computed: true},
						"group_name": schema.StringAttribute{Computed: true},
						"products": schema.ListNestedAttribute{
							Computed: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"product_id":   schema.StringAttribute{Computed: true},
									"price_id":     schema.StringAttribute{Computed: true},
									"name":         schema.StringAttribute{Computed: true},
									"category":     schema.StringAttribute{Computed: true},
									"platform":     schema.StringAttribute{Computed: true},
									"slug":         schema.StringAttribute{Computed: true},
									"cores":        schema.Int64Attribute{Computed: true},
									"memory_gb":    schema.Int64Attribute{Computed: true},
									"storage_gb":   schema.Int64Attribute{Computed: true},
									"bandwidth_gb": schema.Int64Attribute{Computed: true},
									"rate_hourly":  schema.Float64Attribute{Computed: true},
									"rate_monthly": schema.Float64Attribute{Computed: true},
									"region_ids": schema.ListAttribute{
										Computed:    true,
										ElementType: types.StringType,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *serverCatalogDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the full catalog.
func (d *serverCatalogDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	cat, err := d.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	out := serverCatalogDataSourceModel{
		Currency: types.StringValue(cat.Currency),
		Tiers:    make([]catalogTierModel, 0, len(cat.Tiers)),
		Regions:  make([]catalogRegionModel, 0, len(cat.Regions)),
	}

	for i := range cat.Regions {
		reg := cat.Regions[i]
		out.Regions = append(out.Regions, catalogRegionModel{
			ID:        types.StringValue(reg.ID),
			Name:      types.StringValue(reg.Name),
			NameShort: types.StringValue(reg.NameShort),
			Country:   types.StringValue(reg.Country),
			Active:    types.BoolValue(reg.Active),
		})
	}

	for i := range cat.Tiers {
		tier := cat.Tiers[i]
		tm := catalogTierModel{
			GroupID:   types.StringValue(tier.GroupID),
			GroupName: types.StringValue(tier.GroupName),
			Products:  make([]catalogProductModel, 0, len(tier.Products)),
		}

		for j := range tier.Products {
			p := tier.Products[j]

			regionIDs, diags := types.ListValueFrom(ctx, types.StringType, p.RegionIDs)
			resp.Diagnostics.Append(diags...)

			if resp.Diagnostics.HasError() {
				return
			}

			storage := 0
			for _, disk := range p.Specs.Disks {
				storage += disk.SizeGB
			}

			tm.Products = append(tm.Products, catalogProductModel{
				ProductID:   types.StringValue(p.ID),
				PriceID:     types.StringValue(p.PriceID),
				Name:        types.StringValue(p.Name),
				Category:    types.StringValue(p.Type),
				Platform:    types.StringValue(p.PlatformSlug()),
				Slug:        types.StringValue(p.SizeSlug()),
				Cores:       types.Int64Value(int64(p.Specs.CPUCores)),
				MemoryGB:    types.Int64Value(int64(p.Specs.RAMGB)),
				StorageGB:   types.Int64Value(int64(storage)),
				BandwidthGB: types.Int64Value(int64(p.BandwidthGB)),
				RateHourly:  types.Float64Value(p.RateHourly),
				RateMonthly: types.Float64Value(p.RateMonthly),
				RegionIDs:   regionIDs,
			})
		}

		out.Tiers = append(out.Tiers, tm)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
