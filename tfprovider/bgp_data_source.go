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
	_ datasource.DataSource              = (*bgpDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*bgpDataSource)(nil)
)

type bgpDataSource struct {
	client *gigahost.Client
}

type bgpDataSourceModel struct {
	ASNs     []bgpASNDataModel     `tfsdk:"asns"`
	Sessions []bgpSessionDataModel `tfsdk:"sessions"`
}

type bgpASNDataModel struct {
	ID      types.String `tfsdk:"id"`
	ASN     types.String `tfsdk:"asn"`
	Name    types.String `tfsdk:"name"`
	Status  types.String `tfsdk:"status"`
	Country types.String `tfsdk:"country"`
}

type bgpSessionDataModel struct {
	ID           types.String `tfsdk:"id"`
	ASNID        types.String `tfsdk:"asn_id"`
	Status       types.String `tfsdk:"status"`
	IPType       types.String `tfsdk:"ip_type"`
	IPAddress    types.String `tfsdk:"ip_address"`
	NeighborIPv4 types.String `tfsdk:"neighbor_ipv4"`
	NeighborIPv6 types.String `tfsdk:"neighbor_ipv6"`
	DefaultRoute types.Bool   `tfsdk:"default_route"`
}

// NewBGPDataSource constructs the data source.
func NewBGPDataSource() datasource.DataSource { return &bgpDataSource{} }

// Metadata sets the data source type name.
func (d *bgpDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bgp"
}

// Schema returns the Terraform schema.
func (d *bgpDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns all ASNs and BGP sessions for the account.",
		Attributes: map[string]schema.Attribute{
			"asns": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":      schema.StringAttribute{Computed: true},
						"asn":     schema.StringAttribute{Computed: true},
						"name":    schema.StringAttribute{Computed: true},
						"status":  schema.StringAttribute{Computed: true},
						"country": schema.StringAttribute{Computed: true},
					},
				},
			},
			"sessions": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.StringAttribute{Computed: true},
						"asn_id":        schema.StringAttribute{Computed: true},
						"status":        schema.StringAttribute{Computed: true},
						"ip_type":       schema.StringAttribute{Computed: true},
						"ip_address":    schema.StringAttribute{Computed: true},
						"neighbor_ipv4": schema.StringAttribute{Computed: true},
						"neighbor_ipv6": schema.StringAttribute{Computed: true},
						"default_route": schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *bgpDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches everything BGP-related.
func (d *bgpDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	data, err := d.client.BGP.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read BGP data", err.Error())

		return
	}

	out := bgpDataSourceModel{
		ASNs:     make([]bgpASNDataModel, 0, len(data.ASNs)),
		Sessions: make([]bgpSessionDataModel, 0, len(data.Sessions)),
	}

	for _, a := range data.ASNs {
		out.ASNs = append(out.ASNs, bgpASNDataModel{
			ID:      types.StringValue(a.ID),
			ASN:     types.StringValue(a.ASN),
			Name:    types.StringValue(a.Name),
			Status:  types.StringValue(a.Status),
			Country: types.StringValue(a.Country),
		})
	}

	for _, s := range data.Sessions {
		out.Sessions = append(out.Sessions, bgpSessionDataModel{
			ID:           types.StringValue(s.ID),
			ASNID:        types.StringValue(s.ASNID),
			Status:       types.StringValue(s.Status),
			IPType:       types.StringValue(s.IPType),
			IPAddress:    types.StringValue(s.IPAddress),
			NeighborIPv4: types.StringValue(s.NeighborIPv4),
			NeighborIPv6: types.StringValue(s.NeighborIPv6),
			DefaultRoute: types.BoolValue(s.DefaultRoute),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
