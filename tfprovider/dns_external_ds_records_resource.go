package tfprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*dnsExternalDSRecordsResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsExternalDSRecordsResource)(nil)
	_ resource.ResourceWithImportState = (*dnsExternalDSRecordsResource)(nil)
)

// dnsExternalDSRecordsResource manages the DNSSEC DS records that
// Gigahost pushes to Norid on behalf of a customer whose DNS is
// hosted externally.
//
// Typical workflow: point nameservers at Cloudflare (or similar) via
// gigahost_dns_nameservers, enable DNSSEC at the DNS host, copy the
// DS record from there into this resource, and Terraform will keep
// the registry in sync.
//
// The POST endpoint replaces the entire DS record set, so an update
// that changes the list atomically replaces it on Norid too.
type dnsExternalDSRecordsResource struct {
	client *gigahost.Client
}

type dnsExternalDSRecordsModel struct {
	ID        types.String `tfsdk:"id"`
	ZoneID    types.String `tfsdk:"zone_id"`
	DSRecords types.List   `tfsdk:"ds_records"`
}

type dsRecordModel struct {
	KeyTag     types.Int64  `tfsdk:"key_tag"`
	Algorithm  types.Int64  `tfsdk:"algorithm"`
	DigestType types.Int64  `tfsdk:"digest_type"`
	Digest     types.String `tfsdk:"digest"`
}

// dsRecordObjectType is the attr.Type equivalent of dsRecordModel,
// needed whenever we construct a ListValue from scratch.
var dsRecordObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"key_tag":     types.Int64Type,
		"algorithm":   types.Int64Type,
		"digest_type": types.Int64Type,
		"digest":      types.StringType,
	},
}

// NewDNSExternalDSRecordsResource constructs the resource.
func NewDNSExternalDSRecordsResource() resource.Resource { return &dnsExternalDSRecordsResource{} }

// Metadata sets the resource type name.
func (r *dnsExternalDSRecordsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_external_ds_records"
}

// Schema returns the Terraform schema.
func (r *dnsExternalDSRecordsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers DNSSEC DS records with Norid on behalf of a " +
			"Gigahost-registered domain whose DNS is hosted externally. Use together " +
			"with `gigahost_dns_nameservers` and an external DNS host that produces the " +
			"DS record (for example Cloudflare's DNSSEC settings page). The API replaces " +
			"the entire DS record set on each write; destroying the resource clears it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Alias of `zone_id`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "ID of the zone.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ds_records": schema.ListNestedAttribute{
				MarkdownDescription: "The DS records advertised by the external DNS host.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key_tag": schema.Int64Attribute{
							MarkdownDescription: "DNSSEC key tag (0..65535).",
							Required:            true,
						},
						"algorithm": schema.Int64Attribute{
							MarkdownDescription: "Algorithm: 5, 7, 8, 10, 13, 14, 15 or 16.",
							Required:            true,
						},
						"digest_type": schema.Int64Attribute{
							MarkdownDescription: "Digest type: 1=SHA-1, 2=SHA-256, 4=SHA-384.",
							Required:            true,
						},
						"digest": schema.StringAttribute{
							MarkdownDescription: "Hexadecimal digest.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *dnsExternalDSRecordsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create submits the DS records.
func (r *dnsExternalDSRecordsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsExternalDSRecordsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	records, diags := planDSRecords(ctx, plan.DSRecords)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SubmitExternalDSRecords(ctx, plan.ZoneID.ValueString(), records); err != nil {
		resp.Diagnostics.AddError("Failed to submit external DS records", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the DS record set from Gigahost/Norid.
func (r *dnsExternalDSRecordsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsExternalDSRecordsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.DNS.GetExternalDSRecords(ctx, state.ZoneID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			// Zone definitively gone.
			resp.State.RemoveResource(ctx)

			return
		}

		// A 400 from the external DS endpoint means the zone is no longer
		// externally hosted — treat as removed from state, consistent with
		// the documented "not externally hosted" response.
		var apiErr *gigahost.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to read external DS records", err.Error())

		return
	}

	list, diags := dsRecordsToList(ctx, ds.DSRecords)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	state.DSRecords = list

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update re-submits the DS records. The API replaces the set so this
// is identical to Create in effect.
func (r *dnsExternalDSRecordsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsExternalDSRecordsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	records, diags := planDSRecords(ctx, plan.DSRecords)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SubmitExternalDSRecords(ctx, plan.ZoneID.ValueString(), records); err != nil {
		resp.Diagnostics.AddError("Failed to update external DS records", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete clears the DS record set by submitting an empty list. The
// API accepts this as a signal to withdraw DNSSEC at the registry.
func (r *dnsExternalDSRecordsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsExternalDSRecordsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// An empty slice triggers client-side validation; use SetDNSSEC(false)
	// as the semantic equivalent — turn off DNSSEC at the registrar.
	if err := r.client.DNS.SetDNSSEC(ctx, state.ZoneID.ValueString(), false); err != nil {
		resp.Diagnostics.AddError("Failed to clear external DS records", err.Error())

		return
	}
}

// ImportState imports by zone ID or zone name.
func (r *dnsExternalDSRecordsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, err := resolveZoneIdentifier(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unknown zone in import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID)...)
}
