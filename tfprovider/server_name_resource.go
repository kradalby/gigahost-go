package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*serverNameResource)(nil)
	_ resource.ResourceWithConfigure   = (*serverNameResource)(nil)
	_ resource.ResourceWithImportState = (*serverNameResource)(nil)
)

// serverNameResource sets the human-readable label ("name") on a
// server. Servers themselves cannot be created via the API; this is
// the one mutable attribute on an existing server.
type serverNameResource struct {
	client *gigahost.Client
}

type serverNameModel struct {
	ID       types.String `tfsdk:"id"`
	ServerID types.String `tfsdk:"server_id"`
	Name     types.String `tfsdk:"name"`
}

// NewServerNameResource constructs the resource.
func NewServerNameResource() resource.Resource { return &serverNameResource{} }

// Metadata sets the resource type name.
func (r *serverNameResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_name"
}

// Schema returns the Terraform schema.
func (r *serverNameResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Sets the descriptive label on an existing server. " +
			"Destroying this resource resets the label to the server's auto-generated hostname.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Alias of `server_id` to satisfy the Terraform resource model.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the server.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Descriptive name / label for the server.",
				Required:            true,
			},
		},
	}
}

// Configure receives the shared client.
func (r *serverNameResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create applies the label.
func (r *serverNameResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverNameModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Servers.UpdateName(ctx, plan.ServerID.ValueString(), plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update server name", err.Error())

		return
	}

	plan.ID = plan.ServerID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the label.
func (r *serverNameResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverNameModel
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

	state.Name = types.StringValue(srv.Label)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update applies the new label.
func (r *serverNameResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverNameModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Servers.UpdateName(ctx, plan.ServerID.ValueString(), plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update server name", err.Error())

		return
	}

	plan.ID = plan.ServerID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete resets the label to the server's hostname.
func (r *serverNameResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverNameModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	srv, err := r.client.Servers.Get(ctx, state.ServerID.ValueString())
	if err != nil {
		// Server gone, nothing to revert.
		return
	}

	if err := r.client.Servers.UpdateName(ctx, state.ServerID.ValueString(), srv.Hostname); err != nil {
		resp.Diagnostics.AddError("Failed to revert server name", err.Error())

		return
	}
}

// ImportState imports by server ID.
func (r *serverNameResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
