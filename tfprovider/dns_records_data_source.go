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
	_ datasource.DataSource              = (*dnsRecordsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsRecordsDataSource)(nil)
)

// dnsRecordsDataSource lists every DNS record in a zone, for iterating over
// records managed outside Terraform.
type dnsRecordsDataSource struct {
	client *gigahost.Client
}

type dnsRecordEntryModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Value    types.String `tfsdk:"value"`
	TTL      types.Int64  `tfsdk:"ttl"`
	Priority types.Int64  `tfsdk:"priority"`
}

type dnsRecordsDataSourceModel struct {
	Zone    types.String          `tfsdk:"zone"`
	Records []dnsRecordEntryModel `tfsdk:"records"`
}

// NewDNSRecordsDataSource constructs the data source.
func NewDNSRecordsDataSource() datasource.DataSource { return &dnsRecordsDataSource{} }

// Metadata sets the data source type name.
func (d *dnsRecordsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_records"
}

// Schema returns the Terraform schema.
func (d *dnsRecordsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns every DNS record in a zone, by zone name or ID.",
		Attributes: map[string]schema.Attribute{
			"zone": schema.StringAttribute{
				MarkdownDescription: "Zone name (e.g. `example.com`) or zone ID.",
				Required:            true,
			},
			"records": schema.ListNestedAttribute{
				MarkdownDescription: "Every record in the zone.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":       schema.StringAttribute{MarkdownDescription: "Unique identifier.", Computed: true},
						"name":     schema.StringAttribute{MarkdownDescription: "Record name, relative to the zone. `@` is the apex.", Computed: true},
						"type":     schema.StringAttribute{MarkdownDescription: "Record type, e.g. `A`, `AAAA`, `CNAME`, `MX`, `TXT`.", Computed: true},
						"value":    schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
						"ttl":      schema.Int64Attribute{MarkdownDescription: "Time to live in seconds.", Computed: true},
						"priority": schema.Int64Attribute{MarkdownDescription: "Priority; used by MX records.", Computed: true},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (d *dnsRecordsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read resolves the zone and lists its records.
func (d *dnsRecordsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dnsRecordsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	zoneID, err := resolveZoneIdentifier(ctx, d.client, config.Zone.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve DNS zone", err.Error())

		return
	}

	records, err := d.client.DNS.ListRecords(ctx, zoneID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS records", err.Error())

		return
	}

	config.Records = make([]dnsRecordEntryModel, 0, len(records))

	for _, rec := range records {
		entry := dnsRecordEntryModel{
			ID:       types.StringValue(rec.ID),
			Name:     types.StringValue(rec.Name),
			Type:     types.StringValue(string(rec.Type)),
			Value:    types.StringValue(rec.Value),
			TTL:      types.Int64Value(int64(rec.TTL)),
			Priority: types.Int64Null(),
		}

		if rec.Priority != nil {
			entry.Priority = types.Int64Value(int64(*rec.Priority))
		}

		config.Records = append(config.Records, entry)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
