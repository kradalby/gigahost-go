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
	_ datasource.DataSource              = (*dnsZonesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsZonesDataSource)(nil)
)

type dnsZonesDataSource struct {
	client *gigahost.Client
}

type dnsZonesDataSourceModel struct {
	Zones []dnsZoneModel `tfsdk:"zones"`
}

// NewDNSZonesDataSource constructs the data source.
func NewDNSZonesDataSource() datasource.DataSource { return &dnsZonesDataSource{} }

// Metadata sets the data source type name.
func (d *dnsZonesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zones"
}

// Schema returns the Terraform schema.
func (d *dnsZonesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns all DNS zones on your gigahost.no account.",
		Attributes: map[string]schema.Attribute{
			"zones": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.StringAttribute{Computed: true},
						"name":          schema.StringAttribute{Computed: true},
						"type":          schema.StringAttribute{Computed: true},
						"active":        schema.BoolAttribute{Computed: true},
						"protected":     schema.BoolAttribute{Computed: true},
						"is_registered": schema.BoolAttribute{Computed: true},
						"registrar":     schema.StringAttribute{Computed: true},
						"external_dns":  schema.BoolAttribute{Computed: true},
						"record_count":  schema.Int64Attribute{Computed: true},
						"updated_at":    schema.Int64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *dnsZonesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches all zones.
func (d *dnsZonesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	zones, err := d.client.DNS.ListZones(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS zones", err.Error())

		return
	}

	out := dnsZonesDataSourceModel{Zones: make([]dnsZoneModel, 0, len(zones))}

	for _, z := range zones {
		out.Zones = append(out.Zones, dnsZoneModel{
			ID:           types.StringValue(z.ID),
			Name:         types.StringValue(z.Name),
			Type:         types.StringValue(string(z.Type)),
			Active:       types.BoolValue(z.Active),
			Protected:    types.BoolValue(z.Protected),
			IsRegistered: types.BoolValue(z.IsRegistered),
			Registrar:    types.StringValue(z.Registrar),
			ExternalDNS:  types.BoolValue(z.ExternalDNS),
			RecordCount:  types.Int64Value(int64(z.RecordCount)),
			UpdatedAt:    types.Int64Value(z.UpdatedAt.Unix()),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
