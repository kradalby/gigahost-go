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
	_ resource.Resource                = (*serverRDNSResource)(nil)
	_ resource.ResourceWithConfigure   = (*serverRDNSResource)(nil)
	_ resource.ResourceWithImportState = (*serverRDNSResource)(nil)
)

// serverRDNSResource manages the reverse-DNS entry for an IPv4
// address or IPv6 subnet owned by a server.
type serverRDNSResource struct {
	client *gigahost.Client
}

type serverRDNSModel struct {
	ID       types.String `tfsdk:"id"`
	ServerID types.String `tfsdk:"server_id"`
	IPID     types.String `tfsdk:"ip_id"`
	SubnetID types.String `tfsdk:"subnet_id"`
	DNS      types.String `tfsdk:"dns"`
}

// NewServerRDNSResource constructs the resource.
func NewServerRDNSResource() resource.Resource { return &serverRDNSResource{} }

// Metadata sets the resource type name.
func (r *serverRDNSResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_rdns"
}

// Schema returns the Terraform schema.
func (r *serverRDNSResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Sets the reverse DNS (rDNS) entry for a server IPv4 address " +
			"or delegates rDNS for an IPv6 subnet. Provide exactly one of `ip_id` or " +
			"`subnet_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `<server_id>/<ip_id or subnet_id>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the owning server.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ip_id": schema.StringAttribute{
				MarkdownDescription: "IPv4 address ID (set this or `subnet_id`).",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"subnet_id": schema.StringAttribute{
				MarkdownDescription: "IPv6 subnet ID (set this or `ip_id`).",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dns": schema.StringAttribute{
				MarkdownDescription: "Reverse DNS hostname.",
				Required:            true,
			},
		},
	}
}

// Configure receives the shared client.
func (r *serverRDNSResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create applies the rDNS entry.
func (r *serverRDNSResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverRDNSModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if plan.IPID.ValueString() == "" && plan.SubnetID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid configuration", "Exactly one of ip_id or subnet_id is required")

		return
	}

	if err := r.client.Servers.UpdateReverse(ctx, plan.ServerID.ValueString(), gigahost.UpdateReverseRequest{
		IPID:     plan.IPID.ValueString(),
		SubnetID: plan.SubnetID.ValueString(),
		DNS:      plan.DNS.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to update reverse DNS", err.Error())

		return
	}

	key := plan.IPID.ValueString()
	if key == "" {
		key = plan.SubnetID.ValueString()
	}

	plan.ID = types.StringValue(plan.ServerID.ValueString() + "/" + key)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes from the server record.
func (r *serverRDNSResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverRDNSModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	srv, err := r.client.Servers.Get(ctx, state.ServerID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to read server", err.Error())

		return
	}

	if ipID := state.IPID.ValueString(); ipID != "" {
		for _, ip := range srv.IPs {
			if ip.ID == ipID {
				state.DNS = types.StringValue(ip.Reverse)

				resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

				return
			}
		}

		// IP was detached out-of-band — definitive absence.
		resp.State.RemoveResource(ctx)

		return
	} else if subnetID := state.SubnetID.ValueString(); subnetID != "" {
		for _, ip := range srv.IPs {
			if ip.SubnetID == subnetID {
				state.DNS = types.StringValue(ip.Reverse)

				resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

				return
			}
		}

		// Subnet was detached out-of-band — definitive absence.
		resp.State.RemoveResource(ctx)

		return
	}
}

// Update applies a new rDNS entry.
func (r *serverRDNSResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverRDNSModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Servers.UpdateReverse(ctx, plan.ServerID.ValueString(), gigahost.UpdateReverseRequest{
		IPID:     plan.IPID.ValueString(),
		SubnetID: plan.SubnetID.ValueString(),
		DNS:      plan.DNS.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to update reverse DNS", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete clears the rDNS entry (sets it to an empty string, which the
// API interprets as the default).
func (r *serverRDNSResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverRDNSModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Servers.UpdateReverse(ctx, state.ServerID.ValueString(), gigahost.UpdateReverseRequest{
		IPID:     state.IPID.ValueString(),
		SubnetID: state.SubnetID.ValueString(),
		DNS:      "",
	}); err != nil {
		// Best-effort: removing a server out-of-band leaves nothing to reset.
		_ = err
	}
}

// ImportState accepts `<server_id>/<ip_id-or-subnet_id>`.
func (r *serverRDNSResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseImportID(req.ID, "server_id", "ip_or_subnet_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())

		return
	}

	serverID := parts[0]
	candidateID := parts[1]

	srv, srvErr := r.client.Servers.Get(ctx, serverID)
	if srvErr != nil {
		resp.Diagnostics.AddError("Failed to read server during import", srvErr.Error())

		return
	}

	// Classify candidateID: ip_id or subnet_id.
	for _, ip := range srv.IPs {
		if ip.ID == candidateID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), serverID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ip_id"), candidateID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), serverID+"/"+candidateID)...)

			return
		}
	}

	for _, ip := range srv.IPs {
		if ip.SubnetID == candidateID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), serverID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("subnet_id"), candidateID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), serverID+"/"+candidateID)...)

			return
		}
	}

	// Neither matched — build a helpful list of valid IDs.
	var available []string

	for _, ip := range srv.IPs {
		if ip.ID != "" {
			available = append(available, "ip_id="+ip.ID)
		}

		if ip.SubnetID != "" {
			available = append(available, "subnet_id="+ip.SubnetID)
		}
	}

	resp.Diagnostics.AddError(
		"Import ID not found",
		fmt.Sprintf("%q is neither an ip_id nor a subnet_id on server %s. Available: %s",
			candidateID, serverID, strings.Join(available, ", ")),
	)
}
