package tfprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*serverIPv4Resource)(nil)
	_ resource.ResourceWithConfigure   = (*serverIPv4Resource)(nil)
	_ resource.ResourceWithImportState = (*serverIPv4Resource)(nil)
)

// ipOrderSettleTimeout caps how long Create waits for an ordered IP to appear
// in the server's IP list.
const ipOrderSettleTimeout = 60 * time.Second

const ipOrderPollInterval = 3 * time.Second

// serverIPv4Resource orders an additional IPv4 address onto a server. The
// Gigahost API can order an IP (POST /servers/{id}/ipv4) but has no endpoint to
// release one, so destroy cannot free the address — see deletion_policy.
type serverIPv4Resource struct {
	client *gigahost.Client

	// settleTimeout/pollInterval pace the post-order wait; tests shrink them.
	settleTimeout time.Duration
	pollInterval  time.Duration
}

type serverIPv4Model struct {
	ID             types.String `tfsdk:"id"`
	ServerID       types.String `tfsdk:"server_id"`
	Type           types.String `tfsdk:"type"`
	DeletionPolicy types.String `tfsdk:"deletion_policy"`

	Address types.String `tfsdk:"address"`
	Gateway types.String `tfsdk:"gateway"`
	Netmask types.String `tfsdk:"netmask"`
	Version types.String `tfsdk:"version"`
}

// NewServerIPv4Resource constructs the resource.
func NewServerIPv4Resource() resource.Resource { return &serverIPv4Resource{} }

func (r *serverIPv4Resource) settle() time.Duration {
	if r.settleTimeout > 0 {
		return r.settleTimeout
	}

	return ipOrderSettleTimeout
}

func (r *serverIPv4Resource) poll() time.Duration {
	if r.pollInterval > 0 {
		return r.pollInterval
	}

	return ipOrderPollInterval
}

// Metadata sets the resource type name.
func (r *serverIPv4Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_ipv4"
}

// Schema returns the Terraform schema.
func (r *serverIPv4Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Orders an additional IPv4 address onto a `gigahost_server`. Wire the " +
			"`id` into `gigahost_server_rdns.ip_id` or `gigahost_bgp_session`.\n\n" +
			"**The Gigahost API cannot release an IP** (it can only order one), so `terraform " +
			"destroy` of this resource alone cannot free the address. `deletion_policy` controls " +
			"what destroy does. Destroying the whole `gigahost_server` does free its IPs.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "IP ID (the API's `ip_id`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the owning server. Changing it replaces the resource.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "IP kind: `l3` (routed, default) or `l2` (non-routed, layer 2). " +
					"Changing it replaces the resource.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString(string(gigahost.IPTypeL3)),
				Validators: []validator.String{stringvalidator.OneOf(string(gigahost.IPTypeL2), string(gigahost.IPTypeL3))},
				PlanModifiers: []planmodifier.String{
					requiresReplaceUnlessAdoptingStr(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"deletion_policy": schema.StringAttribute{
				MarkdownDescription: "What `terraform destroy` does, since the API cannot release an " +
					"IP: `retain` (default) removes the resource from state and warns that the address " +
					"stays allocated and billed; `error` refuses to destroy.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("retain"),
				Validators: []validator.String{stringvalidator.OneOf("retain", "error")},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The assigned IPv4 address.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "Gateway for the address.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"netmask": schema.StringAttribute{
				MarkdownDescription: "Netmask for the address.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "IP version reported by the API (`4`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// Configure receives the shared client.
func (r *serverIPv4Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create orders an IP and identifies the newly assigned address by diffing the
// server's IP list before and after, serialized per server so concurrent orders
// do not race the diff.
func (r *serverIPv4Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverIPv4Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	serverID := plan.ServerID.ValueString()

	defer lockServer(serverID)()

	before, err := r.client.Servers.Get(ctx, serverID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read server before ordering IP",
			fmt.Sprintf("server %s: %v", serverID, err))

		return
	}

	beforeIDs := ipIDSet(before.IPs)

	if oerr := r.client.Servers.OrderIPv4(ctx, serverID, gigahost.IPType(plan.Type.ValueString())); oerr != nil {
		resp.Diagnostics.AddError("Failed to order IPv4",
			fmt.Sprintf("server %s: %v", serverID, oerr))

		return
	}

	ip, err := r.waitForNewIP(ctx, serverID, beforeIDs)
	if err != nil {
		resp.Diagnostics.AddError("Ordered IP did not appear",
			fmt.Sprintf("the order for server %s was accepted but no new IP appeared within %s: %v. "+
				"The IP may still be provisioning — re-run apply to adopt it, or check the control panel.",
				serverID, r.settle(), err))

		return
	}

	applyIPToModel(&plan, ip)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// waitForNewIP polls the server until an IP not present in beforeIDs appears.
func (r *serverIPv4Resource) waitForNewIP(ctx context.Context, serverID string, beforeIDs map[string]bool) (*gigahost.ServerIP, error) {
	ctx, cancel := context.WithTimeout(ctx, r.settle())
	defer cancel()

	ticker := time.NewTicker(r.poll())
	defer ticker.Stop()

	for {
		srv, err := r.client.Servers.Get(ctx, serverID)
		if err == nil {
			if ip := newestIP(beforeIDs, srv.IPs); ip != nil {
				return ip, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Read refreshes the IP from the live server record.
func (r *serverIPv4Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverIPv4Model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	srv, err := r.client.Servers.Get(ctx, state.ServerID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			// The server is gone, so its IPs are gone too.
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to read server",
			fmt.Sprintf("server %s: %v", state.ServerID.ValueString(), err))

		return
	}

	ip := findIPByID(srv.IPs, state.ID.ValueString())
	if ip == nil {
		resp.State.RemoveResource(ctx)

		return
	}

	applyIPToModel(&state, ip)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update applies the only mutable change (deletion_policy); every other input
// forces replacement. type adopts on first apply after import.
func (r *serverIPv4Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverIPv4Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete cannot free the IP (no API), so it honors deletion_policy: retain
// drops the resource from state with a warning; error refuses.
func (r *serverIPv4Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverIPv4Model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if state.DeletionPolicy.ValueString() == "error" {
		resp.Diagnostics.AddError("Cannot release IP",
			fmt.Sprintf("the Gigahost API cannot release IP %s (%s) on server %s. Release it in the "+
				"control panel, then `terraform state rm` this resource. Set deletion_policy = \"retain\" "+
				"to drop it from state instead.",
				state.ID.ValueString(), state.Address.ValueString(), state.ServerID.ValueString()))

		return
	}

	resp.Diagnostics.AddWarning("IP retained (not released)",
		fmt.Sprintf("IP %s (%s) on server %s cannot be released via the API and remains allocated and "+
			"billed. Release it in the Gigahost control panel if it is no longer needed.",
			state.ID.ValueString(), state.Address.ValueString(), state.ServerID.ValueString()))
}

// ImportState imports by "<server_id>/<ip_id>"; Read fills the rest and the
// adoption-aware type modifier keeps the null type from forcing a replace.
func (r *serverIPv4Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseImportID(req.ID, "server_id", "ip_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// ipIDSet returns the set of IP IDs in a server's IP list.
func ipIDSet(ips []gigahost.ServerIP) map[string]bool {
	set := make(map[string]bool, len(ips))
	for i := range ips {
		set[ips[i].ID] = true
	}

	return set
}

// newestIP returns the IP whose ID is not in before, preferring an IPv4
// (dotted) address. Returns nil when nothing is new.
func newestIP(before map[string]bool, after []gigahost.ServerIP) *gigahost.ServerIP {
	var fallback *gigahost.ServerIP

	for i := range after {
		ip := &after[i]
		if ip.ID == "" || before[ip.ID] {
			continue
		}

		if strings.Contains(ip.Address, ".") {
			return ip
		}

		if fallback == nil {
			fallback = ip
		}
	}

	return fallback
}

// findIPByID returns the IP with the given ID, or nil.
func findIPByID(ips []gigahost.ServerIP, id string) *gigahost.ServerIP {
	for i := range ips {
		if ips[i].ID == id {
			return &ips[i]
		}
	}

	return nil
}

// applyIPToModel copies an IP record onto the model.
func applyIPToModel(m *serverIPv4Model, ip *gigahost.ServerIP) {
	m.ID = types.StringValue(ip.ID)
	m.Address = stringOrNull(ip.Address)
	m.Gateway = stringOrNull(ip.Gateway)
	m.Netmask = stringOrNull(ip.Netmask)
	m.Version = stringOrNull(ip.Version)
}
