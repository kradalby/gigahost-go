package tfprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*dnsDNSSECResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsDNSSECResource)(nil)
	_ resource.ResourceWithImportState = (*dnsDNSSECResource)(nil)
)

// dnsDNSSECResource manages the DNSSEC toggle for a registered domain
// hosted on Gigahost nameservers.
type dnsDNSSECResource struct {
	client *gigahost.Client
}

type dnsDNSSECModel struct {
	ID        types.String `tfsdk:"id"`
	ZoneID    types.String `tfsdk:"zone_id"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	DSRecords types.String `tfsdk:"ds_records"`
}

// NewDNSDNSSECResource constructs the resource.
func NewDNSDNSSECResource() resource.Resource { return &dnsDNSSECResource{} }

// Metadata sets the resource type name.
func (r *dnsDNSSECResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_dnssec"
}

// Schema returns the Terraform schema.
func (r *dnsDNSSECResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Toggles DNSSEC on a registered .no domain hosted on " +
			"Gigahost nameservers. For externally hosted domains use `gigahost_dns_zone` " +
			"nameserver configuration and submit DS records via the DynDNS / Norid channel.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Alias of `zone_id` to satisfy the Terraform resource model.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "ID of the zone.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "`true` to enable DNSSEC; `false` to disable.",
				Required:            true,
			},
			"ds_records": schema.StringAttribute{
				MarkdownDescription: "DS records fetched from /dns/zones/{id}/ds-records. " +
					"Populated when enabled is true.",
				Computed: true,
			},
		},
	}
}

// Configure receives the shared Gigahost client from the provider.
func (r *dnsDNSSECResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create applies the DNSSEC toggle.
func (r *dnsDNSSECResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsDNSSECModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SetDNSSEC(ctx, plan.ZoneID.ValueString(), plan.Enabled.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to toggle DNSSEC", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	if plan.Enabled.ValueBool() {
		if ds, err := r.client.DNS.GetDSRecords(ctx, plan.ZoneID.ValueString()); err == nil && ds != nil {
			plan.DSRecords = types.StringValue(ds.DSRecords)
		}
	} else {
		plan.DSRecords = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the DNSSEC state from the API.
func (r *dnsDNSSECResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsDNSSECModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.DNS.GetDSRecords(ctx, state.ZoneID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			// Zone no longer exists.
			resp.State.RemoveResource(ctx)

			return
		}

		// A 400 from /ds-records signals DNSSEC is not enabled on this zone,
		// not a transport or zone-not-found error.
		var apiErr *gigahost.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
			state.DSRecords = types.StringNull()
			state.Enabled = types.BoolValue(false)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

			return
		}

		resp.Diagnostics.AddError("Failed to read DNSSEC state", err.Error())

		return
	}

	state.DSRecords = types.StringValue(ds.DSRecords)
	state.Enabled = types.BoolValue(ds.DSRecords != "")

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update re-applies the DNSSEC flag.
func (r *dnsDNSSECResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsDNSSECModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SetDNSSEC(ctx, plan.ZoneID.ValueString(), plan.Enabled.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to toggle DNSSEC", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	if plan.Enabled.ValueBool() {
		if ds, err := r.client.DNS.GetDSRecords(ctx, plan.ZoneID.ValueString()); err == nil && ds != nil {
			plan.DSRecords = types.StringValue(ds.DSRecords)
		}
	} else {
		plan.DSRecords = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete disables DNSSEC on the zone.
func (r *dnsDNSSECResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsDNSSECModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SetDNSSEC(ctx, state.ZoneID.ValueString(), false); err != nil {
		resp.Diagnostics.AddError("Failed to disable DNSSEC", err.Error())

		return
	}
}

// ImportState imports by zone ID or zone name.
func (r *dnsDNSSECResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, err := resolveZoneIdentifier(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unknown zone in import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID)...)
}
