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
	Status    types.String `tfsdk:"status"`
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
				MarkdownDescription: "Every server on the account.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{MarkdownDescription: "Unique identifier.", Computed: true},
						"hostname":   schema.StringAttribute{MarkdownDescription: "Hostname configured on the server.", Computed: true},
						"label":      schema.StringAttribute{MarkdownDescription: "User-assigned label.", Computed: true},
						"type":       schema.StringAttribute{MarkdownDescription: "Server type reported by the API.", Computed: true},
						"vps_type":   schema.StringAttribute{MarkdownDescription: "Virtualisation type reported by the API.", Computed: true},
						"location":   schema.StringAttribute{MarkdownDescription: "Datacentre location reported by the API.", Computed: true},
						"cores":      schema.Int64Attribute{MarkdownDescription: "Virtual CPU cores.", Computed: true},
						"ram":        schema.Int64Attribute{MarkdownDescription: "Memory in GB.", Computed: true},
						"bandwidth":  schema.Int64Attribute{MarkdownDescription: "Included bandwidth in GB.", Computed: true},
						"primary_ip": schema.StringAttribute{MarkdownDescription: "Primary IPv4 address.", Computed: true},
						"status": schema.StringAttribute{
							MarkdownDescription: "Power and provisioning state: `running`, `off`, `installing` or `rescue`.",
							Computed:            true,
						},
						"created_at": schema.Int64Attribute{MarkdownDescription: "When the server was created (Unix seconds).", Computed: true},
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
			Status:    types.StringValue(serverStatusString(&s)),
			CreatedAt: unixOrNull(s.CreatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
