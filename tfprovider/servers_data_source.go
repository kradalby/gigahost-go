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
	_ datasource.DataSource              = (*serversDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*serversDataSource)(nil)
)

type serversDataSource struct {
	client *gigahost.Client
}

type serverModel struct {
	ID        types.String `tfsdk:"id"`
	Hostname  types.String `tfsdk:"hostname"`
	Label     types.String `tfsdk:"label"`
	Type      types.String `tfsdk:"type"`
	VPSType   types.String `tfsdk:"vps_type"`
	Location  types.String `tfsdk:"location"`
	Cores     types.Int64  `tfsdk:"cores"`
	RAM       types.Int64  `tfsdk:"ram"`
	Bandwidth types.Int64  `tfsdk:"bandwidth"`
	PrimaryIP types.String `tfsdk:"primary_ip"`
	Status    types.Bool   `tfsdk:"status"`
	CreatedAt types.Int64  `tfsdk:"created_at"`
}

type serversDataSourceModel struct {
	Servers []serverModel `tfsdk:"servers"`
}

// NewServersDataSource constructs the data source.
func NewServersDataSource() datasource.DataSource { return &serversDataSource{} }

// Metadata sets the type name.
func (d *serversDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_servers"
}

// Schema returns the Terraform schema.
func (d *serversDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns all servers on your gigahost.no account.",
		Attributes: map[string]schema.Attribute{
			"servers": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true},
						"hostname":   schema.StringAttribute{Computed: true},
						"label":      schema.StringAttribute{Computed: true},
						"type":       schema.StringAttribute{Computed: true},
						"vps_type":   schema.StringAttribute{Computed: true},
						"location":   schema.StringAttribute{Computed: true},
						"cores":      schema.Int64Attribute{Computed: true},
						"ram":        schema.Int64Attribute{Computed: true},
						"bandwidth":  schema.Int64Attribute{Computed: true},
						"primary_ip": schema.StringAttribute{Computed: true},
						"status":     schema.BoolAttribute{Computed: true},
						"created_at": schema.Int64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure captures the shared gigahost client.
func (d *serversDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches all servers.
func (d *serversDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	servers, err := d.client.Servers.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list servers", err.Error())

		return
	}

	out := serversDataSourceModel{Servers: make([]serverModel, 0, len(servers))}

	for _, s := range servers {
		out.Servers = append(out.Servers, serverModel{
			ID:        types.StringValue(s.ID),
			Hostname:  types.StringValue(s.Hostname),
			Label:     types.StringValue(s.Label),
			Type:      types.StringValue(s.Type),
			VPSType:   types.StringValue(s.VPSType),
			Location:  types.StringValue(s.Location),
			Cores:     types.Int64Value(int64(s.Cores)),
			RAM:       types.Int64Value(int64(s.RAM)),
			Bandwidth: types.Int64Value(int64(s.Bandwidth)),
			PrimaryIP: types.StringValue(s.PrimaryIP),
			Status:    types.BoolValue(s.Status),
			CreatedAt: types.Int64Value(s.CreatedAt.Unix()),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
