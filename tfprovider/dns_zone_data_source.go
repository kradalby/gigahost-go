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
	_ datasource.DataSource              = (*dnsZoneDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsZoneDataSource)(nil)
)

type dnsZoneDataSource struct {
	client *gigahost.Client
}

// NewDNSZoneDataSource constructs the single-zone data source.
func NewDNSZoneDataSource() datasource.DataSource { return &dnsZoneDataSource{} }

// Metadata sets the data source type name.
func (d *dnsZoneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

// Schema returns the Terraform schema. The zone is looked up by either
// `id` or `name`; at least one must be set.
func (d *dnsZoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single DNS zone by `id` or `name`.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{MarkdownDescription: "Zone ID.", Optional: true, Computed: true},
			"name":          schema.StringAttribute{MarkdownDescription: "Zone (domain) name.", Optional: true, Computed: true},
			"type":          schema.StringAttribute{MarkdownDescription: "Zone type: `NATIVE`, `MASTER` or `SLAVE`.", Computed: true},
			"active":        schema.BoolAttribute{MarkdownDescription: "True when the zone is active.", Computed: true},
			"protected":     schema.BoolAttribute{MarkdownDescription: "True when the zone is registered and cannot be deleted through the API.", Computed: true},
			"is_registered": schema.BoolAttribute{MarkdownDescription: "True when the domain is registered through Gigahost.", Computed: true},
			"registrar":     schema.StringAttribute{MarkdownDescription: "Domain registrar, when the domain is registered.", Computed: true},
			"external_dns":  schema.BoolAttribute{MarkdownDescription: "True when the zone is served by external nameservers.", Computed: true},
			"record_count":  schema.Int64Attribute{MarkdownDescription: "Number of records in the zone.", Computed: true},
			"updated_at":    schema.Int64Attribute{MarkdownDescription: "When the zone was last updated (Unix seconds).", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *dnsZoneDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read looks up and returns the zone.
func (d *dnsZoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dnsZoneModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.ID.ValueString() == "" && cfg.Name.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid query", "Set at least one of `id` or `name`")

		return
	}

	zones, err := d.client.DNS.ListZones(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS zones", err.Error())

		return
	}

	wantID := cfg.ID.ValueString()
	wantName := cfg.Name.ValueString()

	for _, z := range zones {
		if (wantID != "" && z.ID == wantID) || (wantName != "" && z.Name == wantName) {
			out := dnsZoneModel{
				ID:           types.StringValue(z.ID),
				Name:         types.StringValue(z.Name),
				Type:         types.StringValue(string(z.Type)),
				Active:       types.BoolValue(z.Active),
				Protected:    types.BoolValue(z.Protected),
				IsRegistered: types.BoolValue(z.IsRegistered),
				Registrar:    types.StringValue(z.Registrar),
				ExternalDNS:  types.BoolValue(z.ExternalDNS),
				RecordCount:  types.Int64Value(int64(z.RecordCount)),
				UpdatedAt:    unixOrNull(z.UpdatedAt),
			}

			resp.Diagnostics.Append(resp.State.Set(ctx, out)...)

			return
		}
	}

	resp.Diagnostics.AddError(
		"DNS zone not found",
		fmt.Sprintf("no zone matched id=%q name=%q", wantID, wantName),
	)
}
