package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*bgpSessionResource)(nil)
	_ resource.ResourceWithConfigure   = (*bgpSessionResource)(nil)
	_ resource.ResourceWithImportState = (*bgpSessionResource)(nil)
)

// bgpSessionResource manages BGP peering sessions.
type bgpSessionResource struct {
	client *gigahost.Client
}

type bgpSessionModel struct {
	ID           types.String `tfsdk:"id"`
	ASNID        types.String `tfsdk:"asn_id"`
	Redundant    types.Bool   `tfsdk:"redundant"`
	DefaultRoute types.Bool   `tfsdk:"default_route"`
	IPv4IPID     types.String `tfsdk:"ipv4_ip_id"`
	IPv6IPID     types.String `tfsdk:"ipv6_ip_id"`
	Status       types.String `tfsdk:"status"`
}

// NewBGPSessionResource constructs the resource.
func NewBGPSessionResource() resource.Resource { return &bgpSessionResource{} }

// Metadata sets the resource type name.
func (r *bgpSessionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bgp_session"
}

// Schema returns the Terraform schema.
func (r *bgpSessionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A BGP peering session for an approved ASN. Session " +
			"deletion is asynchronous server-side.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Session ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"asn_id": schema.StringAttribute{
				MarkdownDescription: "ASN record ID (from `gigahost_bgp_asn.id`).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			// No update API exists for sessions; all mutable attributes require replacement.
			"redundant": schema.BoolAttribute{
				MarkdownDescription: "Create redundant sessions.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"default_route": schema.BoolAttribute{
				MarkdownDescription: "Receive the default route.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"ipv4_ip_id": schema.StringAttribute{
				MarkdownDescription: "IP ID for IPv4 peering. At least one of ipv4_ip_id / ipv6_ip_id is required.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ipv6_ip_id": schema.StringAttribute{
				MarkdownDescription: "IP ID for IPv6 peering.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{MarkdownDescription: "Current session status.", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *bgpSessionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create opens the BGP session.
func (r *bgpSessionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bgpSessionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createReq := gigahost.CreateBGPSessionRequest{
		Redundant:    plan.Redundant.ValueBool(),
		DefaultRoute: plan.DefaultRoute.ValueBool(),
		IPIDv4:       plan.IPv4IPID.ValueString(),
		IPIDv6:       plan.IPv6IPID.ValueString(),
	}

	if err := r.client.BGP.CreateSession(ctx, plan.ASNID.ValueString(), createReq); err != nil {
		resp.Diagnostics.AddError("Failed to create BGP session", err.Error())

		return
	}

	if _, err := r.refreshModel(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to look up BGP session after create", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes session state.
func (r *bgpSessionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bgpSessionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.refreshModel(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read BGP session", err.Error())

		return
	}

	if !found {
		// Session deleted out-of-band; remove from state.
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is unreachable — asn_id, ipv4_ip_id, ipv6_ip_id, redundant and
// default_route all carry RequiresReplace; no update API exists for sessions.
func (r *bgpSessionResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

// Delete marks the session for deletion.
func (r *bgpSessionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bgpSessionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.BGP.DeleteSession(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete BGP session", err.Error())

		return
	}
}

// ImportState imports by session ID.
func (r *bgpSessionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// refreshModel fetches /bgp and populates m from the matching session.
// Returns (true, nil) when found, (false, nil) when absent, (false, err) for
// transport failures.
//
// API contract assumption: each session record carries exactly one IP ID; the
// ipv4 and ipv6 fields are mutually exclusive — a single record never has both
// set simultaneously.
//
// When m.ASNID is empty (just-imported state), it matches by session ID and
// backfills asn_id, ipv4_ip_id/ipv6_ip_id and default_route from the record.
// In the normal path it matches by asn_id + ip_id and populates default_route.
func (r *bgpSessionResource) refreshModel(ctx context.Context, m *bgpSessionModel) (bool, error) {
	data, err := r.client.BGP.Get(ctx)
	if err != nil {
		return false, err
	}

	asnID := m.ASNID.ValueString()

	// Import path: ASNID not yet in state — match by session ID.
	if asnID == "" {
		sessionID := m.ID.ValueString()

		for _, s := range data.Sessions {
			if s.ID != sessionID {
				continue
			}

			m.ASNID = types.StringValue(s.ASNID)
			m.Status = types.StringValue(s.Status)
			m.DefaultRoute = types.BoolValue(s.DefaultRoute)

			switch s.IPType {
			case "ipv4":
				m.IPv4IPID = types.StringValue(s.IPID)
			case "ipv6":
				m.IPv6IPID = types.StringValue(s.IPID)
			}

			return true, nil
		}

		return false, nil
	}

	// Normal path: match by asn_id + ip_id.
	wantedIPv4 := m.IPv4IPID.ValueString()
	wantedIPv6 := m.IPv6IPID.ValueString()

	for _, s := range data.Sessions {
		if s.ASNID != asnID {
			continue
		}

		if wantedIPv4 != "" && s.IPID == wantedIPv4 {
			m.ID = types.StringValue(s.ID)
			m.Status = types.StringValue(s.Status)
			m.DefaultRoute = types.BoolValue(s.DefaultRoute)

			return true, nil
		}

		if wantedIPv6 != "" && s.IPID == wantedIPv6 {
			m.ID = types.StringValue(s.ID)
			m.Status = types.StringValue(s.Status)
			m.DefaultRoute = types.BoolValue(s.DefaultRoute)

			return true, nil
		}
	}

	return false, nil
}
