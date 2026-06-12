package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ datasource.DataSource                     = (*serverDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*serverDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*serverDataSource)(nil)
)

type serverDataSource struct {
	client *gigahost.Client
}

type serverIPModel struct {
	ID       types.String `tfsdk:"id"`
	SubnetID types.String `tfsdk:"subnet_id"`
	Version  types.String `tfsdk:"version"`
	Address  types.String `tfsdk:"address"`
	Reverse  types.String `tfsdk:"reverse"`
	Type     types.String `tfsdk:"type"`
}

type singleServerModel struct {
	ID        types.String    `tfsdk:"id"`
	Hostname  types.String    `tfsdk:"hostname"`
	Label     types.String    `tfsdk:"label"`
	Type      types.String    `tfsdk:"type"`
	VPSType   types.String    `tfsdk:"vps_type"`
	Location  types.String    `tfsdk:"location"`
	Cores     types.Int64     `tfsdk:"cores"`
	RAM       types.Int64     `tfsdk:"ram"`
	Bandwidth types.Int64     `tfsdk:"bandwidth"`
	PrimaryIP types.String    `tfsdk:"primary_ip"`
	Status    types.Bool      `tfsdk:"status"`
	CreatedAt types.Int64     `tfsdk:"created_at"`
	IPs       []serverIPModel `tfsdk:"ips"`
}

// NewServerDataSource constructs the single-server data source.
func NewServerDataSource() datasource.DataSource { return &serverDataSource{} }

// Metadata sets the data source type name.
func (d *serverDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

// ConfigValidators requires exactly one of id / hostname.
func (d *serverDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("hostname"),
		),
	}
}

// Schema returns the Terraform schema.
func (d *serverDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns information about a single server, looked up by `id` or " +
			"`hostname`. The `ips` list carries the IP and subnet IDs needed by " +
			"`gigahost_server_rdns` and `gigahost_bgp_session`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server ID. Exactly one of `id` or `hostname`.",
				Optional:            true,
				Computed:            true,
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "Server hostname (exact, case-insensitive). " +
					"Exactly one of `id` or `hostname`.",
				Optional: true,
				Computed: true,
			},
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
			"ips": schema.ListNestedAttribute{
				MarkdownDescription: "IP addresses assigned to the server.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "IP ID — feeds `gigahost_server_rdns.ip_id` " +
								"and `gigahost_bgp_session.ipv4_ip_id`/`ipv6_ip_id`.",
							Computed: true,
						},
						"subnet_id": schema.StringAttribute{
							MarkdownDescription: "Subnet ID — feeds `gigahost_server_rdns.subnet_id`.",
							Computed:            true,
						},
						"version": schema.StringAttribute{Computed: true},
						"address": schema.StringAttribute{Computed: true},
						"reverse": schema.StringAttribute{Computed: true},
						"type":    schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *serverDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the server by ID or hostname.
func (d *serverDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg singleServerModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ref := cfg.ID.ValueString()
	if ref == "" {
		ref = cfg.Hostname.ValueString()
	}

	if ref == "" {
		resp.Diagnostics.AddError(
			"Empty server reference",
			"id or hostname resolved to an empty string — servers deployed without an explicit hostname cannot be looked up by hostname",
		)

		return
	}

	srv, err := d.client.Servers.Resolve(ctx, ref)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read server", err.Error())

		return
	}

	// The list endpoint omits IPs; fetch the full record when needed.
	if len(srv.IPs) == 0 {
		srv, err = d.client.Servers.Get(ctx, srv.ID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read server", err.Error())

			return
		}
	}

	// Deploy-time hostnames land in srv_name on the live API; srv_hostname
	// stays empty. Surface whichever is set so by-hostname round-trips.
	hostname := srv.Hostname
	if hostname == "" {
		hostname = srv.Name
	}

	out := singleServerModel{
		ID:        types.StringValue(srv.ID),
		Hostname:  types.StringValue(hostname),
		Label:     types.StringValue(srv.Label),
		Type:      types.StringValue(srv.Type),
		VPSType:   types.StringValue(srv.VPSType),
		Location:  types.StringValue(srv.Location),
		Cores:     types.Int64Value(int64(srv.Cores)),
		RAM:       types.Int64Value(int64(srv.RAM)),
		Bandwidth: types.Int64Value(int64(srv.Bandwidth)),
		PrimaryIP: types.StringValue(srv.PrimaryIP),
		Status:    types.BoolValue(srv.Status),
		CreatedAt: types.Int64Value(srv.CreatedAt.Unix()),
		IPs:       make([]serverIPModel, 0, len(srv.IPs)),
	}

	for _, ip := range srv.IPs {
		out.IPs = append(out.IPs, serverIPModel{
			ID:       types.StringValue(ip.ID),
			SubnetID: types.StringValue(ip.SubnetID),
			Version:  types.StringValue(ip.Version),
			Address:  types.StringValue(ip.Address),
			Reverse:  types.StringValue(ip.Reverse),
			Type:     types.StringValue(ip.Type),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, out)...)
}
