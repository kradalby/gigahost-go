package tfprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*dnsRecordResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRecordResource)(nil)
)

// dnsRecordResource manages `gigahost_dns_record`.
type dnsRecordResource struct {
	client *gigahost.Client
}

// dnsRecordModel is the Terraform schema representation. The id is a
// composite `<zone_id>/<record_id>` so Terraform can uniquely address
// records.
type dnsRecordModel struct {
	ID       types.String `tfsdk:"id"`
	RecordID types.String `tfsdk:"record_id"`
	ZoneID   types.String `tfsdk:"zone_id"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Value    types.String `tfsdk:"value"`
	TTL      types.Int64  `tfsdk:"ttl"`
	Priority types.Int64  `tfsdk:"priority"`
}

// NewDNSRecordResource is the constructor registered with the provider.
func NewDNSRecordResource() resource.Resource { return &dnsRecordResource{} }

// Metadata sets the resource type name.
func (r *dnsRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

// Schema returns the Terraform schema.
func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a DNS record within a gigahost.no zone.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `<zone_id>/<record_id>`. " +
					"The API derives the record ID from the record's content, so it " +
					"changes whenever name/type/value change. When importing, the " +
					"zone part may be the zone ID or the zone name.",
				Computed: true,
			},
			"record_id": schema.StringAttribute{
				MarkdownDescription: "API record ID (a content hash; changes when the record content changes).",
				Computed:            true,
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "ID of the containing zone.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Record name (`@` for the zone apex).",
				Optional:            true,
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Record type (A, AAAA, CNAME, MX, TXT, NS, …).",
				Required:            true,
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "Record value. For hostname-valued types (CNAME, MX, NS, SRV, PTR) " +
					"the API returns the target with a trailing dot; if your configured value " +
					"differs only by that dot the state keeps your form, so omitting the dot does " +
					"not produce a perpetual diff. For TXT (and other content-valued types) the " +
					"trailing dot is significant and is preserved verbatim.",
				Required: true,
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "Time-to-live in seconds.",
				Optional:            true,
				Computed:            true,
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Priority (used for MX records).",
				Optional:            true,
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *dnsRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*gigahost.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))

		return
	}

	r.client = client
}

// Create creates the DNS record.
func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	defer lockZone(plan.ZoneID.ValueString())()

	createReq := gigahost.CreateRecordRequest{
		Name:  plan.Name.ValueString(),
		Type:  gigahost.RecordType(plan.Type.ValueString()),
		Value: plan.Value.ValueString(),
		TTL:   int(plan.TTL.ValueInt64()),
	}

	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		p := int(plan.Priority.ValueInt64())
		createReq.Priority = &p
	}

	if err := r.client.DNS.CreateRecord(ctx, plan.ZoneID.ValueString(), createReq); err != nil {
		resp.Diagnostics.AddError("Failed to create DNS record", err.Error())

		return
	}

	// The API does not return the record ID on create; list and find
	// the newly created record by name+type+value.
	records, err := r.client.DNS.ListRecords(ctx, plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up created record", err.Error())

		return
	}

	wanted := matchRecord(records, createReq.Name, string(createReq.Type), createReq.Value)
	if wanted == nil {
		resp.Diagnostics.AddError(
			"Record not found after create",
			"Gigahost returned success but the record was not found on re-query.",
		)

		return
	}

	plan.setFromRecord(plan.ZoneID.ValueString(), wanted)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the record from the API.
func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	records, err := r.client.DNS.ListRecords(ctx, state.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS records", err.Error())

		return
	}

	var found *gigahost.DNSRecord

	for i := range records {
		if records[i].ID == state.RecordID.ValueString() {
			found = &records[i]

			break
		}
	}

	if found == nil {
		// The record no longer exists: remove it from state so
		// Terraform can re-create it on next apply.
		resp.State.RemoveResource(ctx)

		return
	}

	state.Name = types.StringValue(found.Name)
	state.Type = types.StringValue(string(found.Type))
	state.Value = types.StringValue(dnsValueForState(state.Value, found.Value, found.Type))
	state.TTL = types.Int64Value(int64(found.TTL))

	if found.Priority != nil {
		state.Priority = types.Int64Value(int64(*found.Priority))
	} else {
		state.Priority = types.Int64Null()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update replaces the record on the API.
func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	defer lockZone(state.ZoneID.ValueString())()

	updateReq := gigahost.UpdateRecordRequest{
		Name:  plan.Name.ValueString(),
		Type:  gigahost.RecordType(plan.Type.ValueString()),
		Value: plan.Value.ValueString(),
		TTL:   int(plan.TTL.ValueInt64()),
	}

	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		p := int(plan.Priority.ValueInt64())
		updateReq.Priority = &p
	}

	if err := r.client.DNS.UpdateRecord(ctx, state.ZoneID.ValueString(), state.RecordID.ValueString(), updateReq); err != nil {
		resp.Diagnostics.AddError("Failed to update DNS record", err.Error())

		return
	}

	// Re-resolve so every computed field (notably name) is known after apply.
	records, err := r.client.DNS.ListRecords(ctx, state.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up updated record", err.Error())

		return
	}

	wanted := matchRecord(records, plan.Name.ValueString(), plan.Type.ValueString(), plan.Value.ValueString())
	if wanted == nil {
		resp.Diagnostics.AddError(
			"Record not found after update",
			"Gigahost returned success but the record was not found on re-query.",
		)

		return
	}

	plan.setFromRecord(state.ZoneID.ValueString(), wanted)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the record.
func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	defer lockZone(state.ZoneID.ValueString())()

	if err := r.client.DNS.DeleteRecord(
		ctx,
		state.ZoneID.ValueString(),
		state.RecordID.ValueString(),
		state.Name.ValueString(),
		gigahost.RecordType(state.Type.ValueString()),
	); err != nil {
		resp.Diagnostics.AddError("Failed to delete DNS record", err.Error())

		return
	}
}

// ImportState supports `terraform import ... <ZONE_ID|ZONE_NAME>/RECORD_ID`.
func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseImportID(req.ID, "zone", "record_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())

		return
	}

	zoneID, err := resolveZoneIdentifier(ctx, r.client, parts[0])
	if err != nil {
		resp.Diagnostics.AddError(
			"Unknown zone in import ID",
			fmt.Sprintf("Could not resolve zone %q: %s", parts[0], err),
		)

		return
	}

	recordID := parts[1]

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("record_id"), recordID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID+"/"+recordID)...)
}

// setFromRecord copies the API record's identity and computed fields into the
// model, guaranteeing every computed attribute is known after apply.
func (m *dnsRecordModel) setFromRecord(zoneID string, rec *gigahost.DNSRecord) {
	m.RecordID = types.StringValue(rec.ID)
	m.ID = types.StringValue(zoneID + "/" + rec.ID)
	m.Name = types.StringValue(rec.Name)
	m.TTL = types.Int64Value(int64(rec.TTL))

	if rec.Priority != nil {
		m.Priority = types.Int64Value(int64(*rec.Priority))
	} else {
		m.Priority = types.Int64Null()
	}
}

// normalizeDNSValue strips a single trailing dot so values compare equal
// regardless of whether the caller wrote the FQDN form; the API stores
// hostnames without the trailing dot.
func normalizeDNSValue(v string) string {
	return strings.TrimSuffix(v, ".")
}

// hostnameValuedRecordTypes is the set of record types whose value is a
// hostname/FQDN, where a single trailing dot is a notational FQDN terminator
// rather than content. The API returns these with a trailing dot; configs
// usually omit it. Content-valued types (notably TXT, also CAA) are excluded —
// for those the dot is significant and must be preserved verbatim.
var hostnameValuedRecordTypes = map[gigahost.RecordType]bool{
	gigahost.RecordTypeCNAME: true,
	gigahost.RecordTypeMX:    true,
	gigahost.RecordTypeNS:    true,
	gigahost.RecordTypeSRV:   true,
	gigahost.RecordTypePTR:   true,
}

// dnsValueForState decides which form of a record value to store in state after
// a refresh, so hostname-valued records imported/written without a trailing dot
// converge instead of planning a perpetual diff.
//
// The API returns hostname-valued types (CNAME/MX/NS/SRV/PTR) with a trailing
// dot; configs usually omit it. When the prior state value and the API value
// are equal modulo exactly one trailing dot AND the type is hostname-valued,
// the state's existing form is kept so no diff appears. Otherwise — including
// every content-valued type such as TXT, where the dot is significant — the API
// form is stored verbatim. On import (prior state null/empty) the API form is
// always stored.
func dnsValueForState(prior types.String, apiValue string, recordType gigahost.RecordType) string {
	if prior.IsNull() || prior.IsUnknown() {
		return apiValue
	}

	priorValue := prior.ValueString()
	if priorValue == apiValue {
		return apiValue
	}

	if hostnameValuedRecordTypes[recordType] &&
		normalizeDNSValue(priorValue) == normalizeDNSValue(apiValue) {
		return priorValue
	}

	return apiValue
}

// matchRecord finds the record in the slice with the matching name, type and
// value. Name "" / "@" both match the apex; values are compared with trailing
// dots normalised away.
func matchRecord(records []gigahost.DNSRecord, name, recordType, value string) *gigahost.DNSRecord {
	if name == "" {
		name = "@"
	}

	wantValue := normalizeDNSValue(value)

	for i := range records {
		if records[i].Name == name &&
			string(records[i].Type) == recordType &&
			normalizeDNSValue(records[i].Value) == wantValue {
			return &records[i]
		}
	}

	return nil
}
