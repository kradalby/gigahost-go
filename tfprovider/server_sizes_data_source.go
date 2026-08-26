package tfprovider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource              = (*serverSizesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*serverSizesDataSource)(nil)
)

// serverSizesDataSource lists all deployable server sizes as a flat list.
type serverSizesDataSource struct {
	client *gigahost.Client
}

type serverSizesEntryModel struct {
	Slug        types.String  `tfsdk:"slug"`
	Platform    types.String  `tfsdk:"platform"`
	Type        types.String  `tfsdk:"type"`
	Category    types.String  `tfsdk:"category"`
	Name        types.String  `tfsdk:"name"`
	Cores       types.Int64   `tfsdk:"cores"`
	MemoryGB    types.Int64   `tfsdk:"memory_gb"`
	StorageGB   types.Int64   `tfsdk:"storage_gb"`
	BandwidthGB types.Int64   `tfsdk:"bandwidth_gb"`
	RateHourly  types.Float64 `tfsdk:"rate_hourly"`
	RateMonthly types.Float64 `tfsdk:"rate_monthly"`
	Regions     types.List    `tfsdk:"regions"`
}

type serverSizesModel struct {
	Platform types.String            `tfsdk:"platform"`
	Type     types.String            `tfsdk:"type"`
	Region   types.String            `tfsdk:"region"`
	Currency types.String            `tfsdk:"currency"`
	Sizes    []serverSizesEntryModel `tfsdk:"sizes"`
}

// NewServerSizesDataSource constructs the data source.
func NewServerSizesDataSource() datasource.DataSource { return &serverSizesDataSource{} }

// Metadata sets the data source type name.
func (d *serverSizesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_sizes"
}

// Schema returns the Terraform schema.
func (d *serverSizesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every deployable server size as a flat list, optionally " +
			"filtered by `platform`, `type`, or `region`. Same data as `gigahost deploy sizes`. " +
			"For picking exactly one size, prefer `gigahost_server_size`.",
		Attributes: map[string]schema.Attribute{
			"platform": schema.StringAttribute{
				MarkdownDescription: "Only sizes on this platform (`cloud` or `metal`).",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Only sizes of this type slug, e.g. `value`.",
				Optional:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Only sizes offered in this region (slug or name).",
				Optional:            true,
			},
			"currency": schema.StringAttribute{MarkdownDescription: "ISO currency code prices are quoted in.", Computed: true},
			"sizes": schema.ListNestedAttribute{
				MarkdownDescription: "Deployable sizes.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							MarkdownDescription: "Size slug for `gigahost_server.size`.",
							Computed:            true,
						},
						"platform":     schema.StringAttribute{MarkdownDescription: "Platform: `cloud` or `metal`.", Computed: true},
						"type":         schema.StringAttribute{MarkdownDescription: "Type slug, e.g. `value` or `performance`.", Computed: true},
						"category":     schema.StringAttribute{MarkdownDescription: "Catalog category the size belongs to.", Computed: true},
						"name":         schema.StringAttribute{MarkdownDescription: "Product name as shown in the control panel.", Computed: true},
						"cores":        schema.Int64Attribute{MarkdownDescription: "Virtual CPU cores.", Computed: true},
						"memory_gb":    schema.Int64Attribute{MarkdownDescription: "Memory in GB.", Computed: true},
						"storage_gb":   schema.Int64Attribute{MarkdownDescription: "Disk in GB, summed across all disks.", Computed: true},
						"bandwidth_gb": schema.Int64Attribute{MarkdownDescription: "Included bandwidth in GB.", Computed: true},
						"rate_hourly":  schema.Float64Attribute{MarkdownDescription: "Hourly price in the account's currency.", Computed: true},
						"rate_monthly": schema.Float64Attribute{MarkdownDescription: "Monthly price in the account's currency.", Computed: true},
						"regions": schema.ListAttribute{
							MarkdownDescription: "Region slugs the size is offered in.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *serverSizesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read lists deployable sizes from the live catalog.
func (d *serverSizesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config serverSizesModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cat, err := d.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	var regionFilter *gigahost.DeployRegion

	if r := config.Region.ValueString(); r != "" {
		regionFilter, err = cat.FindRegion(r)
		if err != nil {
			resp.Diagnostics.AddError("Unknown region", err.Error())

			return
		}
	}

	platformFilter := strings.ToLower(config.Platform.ValueString())
	typeFilter := strings.ToLower(config.Type.ValueString())

	out := serverSizesModel{
		Platform: config.Platform,
		Type:     config.Type,
		Region:   config.Region,
		Currency: types.StringValue(cat.Currency),
		Sizes:    []serverSizesEntryModel{},
	}

	for i := range cat.Tiers {
		tier := &cat.Tiers[i]
		typeSlug := tier.TypeSlug()

		if typeFilter != "" && typeSlug != typeFilter {
			continue
		}

		for j := range tier.Products {
			p := &tier.Products[j]
			if !p.Deployable() {
				continue
			}

			if platformFilter != "" && p.PlatformSlug() != platformFilter {
				continue
			}

			if regionFilter != nil && !productInRegion(p, regionFilter.ID) {
				continue
			}

			regions, diags := types.ListValueFrom(ctx, types.StringType, productRegionSlugs(cat, p))
			resp.Diagnostics.Append(diags...)

			if resp.Diagnostics.HasError() {
				return
			}

			storage := 0
			for _, disk := range p.Specs.Disks {
				storage += disk.SizeGB
			}

			out.Sizes = append(out.Sizes, serverSizesEntryModel{
				Slug:        types.StringValue(p.SizeSlug()),
				Platform:    types.StringValue(p.PlatformSlug()),
				Type:        types.StringValue(typeSlug),
				Category:    types.StringValue(p.Type),
				Name:        types.StringValue(p.Name),
				Cores:       types.Int64Value(int64(p.Specs.CPUCores)),
				MemoryGB:    types.Int64Value(int64(p.Specs.RAMGB)),
				StorageGB:   types.Int64Value(int64(storage)),
				BandwidthGB: types.Int64Value(int64(p.BandwidthGB)),
				RateHourly:  types.Float64Value(p.RateHourly),
				RateMonthly: types.Float64Value(p.RateMonthly),
				Regions:     regions,
			})
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}

// productInRegion reports whether the product is offered in the region.
func productInRegion(p *gigahost.DeployProduct, regionID string) bool {
	return slices.Contains(p.RegionIDs, regionID)
}
