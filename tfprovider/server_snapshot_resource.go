package tfprovider

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*serverSnapshotResource)(nil)
	_ resource.ResourceWithConfigure   = (*serverSnapshotResource)(nil)
	_ resource.ResourceWithImportState = (*serverSnapshotResource)(nil)
)

const (
	snapshotAppearTimeout = 10 * time.Minute
	snapshotPollInterval  = 10 * time.Second
)

// serverSnapshotResource manages a point-in-time snapshot of a server. A
// snapshot is an immutable entity: changing any input replaces it (new
// snapshot). Creating polls until the snapshot appears; the user-supplied name
// is stored by the API as the snapshot's display name (the API generates a
// separate random internal name).
type serverSnapshotResource struct {
	client *gigahost.Client
}

type serverSnapshotResourceModel struct {
	ID         types.String `tfsdk:"id"`
	ServerID   types.String `tfsdk:"server_id"`
	Name       types.String `tfsdk:"name"`
	SnapshotID types.String `tfsdk:"snapshot_id"`
	State      types.String `tfsdk:"state"`
}

// NewServerSnapshotResource constructs the resource.
func NewServerSnapshotResource() resource.Resource { return &serverSnapshotResource{} }

// Metadata sets the resource type name.
func (r *serverSnapshotResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_snapshot"
}

// Schema returns the Terraform schema.
func (r *serverSnapshotResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a point-in-time snapshot of a server. Snapshots are immutable; changing " +
			"`name` or `server_id` replaces the snapshot. Destroying the resource deletes the snapshot.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `<server_id>/<snapshot_id>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the server to snapshot.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Descriptive name for the snapshot (stored as its display name).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"snapshot_id": schema.StringAttribute{
				MarkdownDescription: "API snapshot ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Snapshot state (pending, completed).",
				Computed:            true,
			},
		},
	}
}

// Configure receives the shared client.
func (r *serverSnapshotResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create takes the snapshot and waits for it to appear.
func (r *serverSnapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverSnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	serverID := plan.ServerID.ValueString()
	name := plan.Name.ValueString()

	if err := r.client.Snapshots.Create(ctx, serverID, name); err != nil {
		resp.Diagnostics.AddError("Failed to create snapshot", err.Error())

		return
	}

	snap := r.waitForSnapshot(ctx, serverID, name)
	if snap == nil {
		resp.Diagnostics.AddError("Snapshot did not appear",
			fmt.Sprintf("the snapshot %q was not listed on server %s within %s", name, serverID, snapshotAppearTimeout))

		return
	}

	r.setSnapshot(&plan, serverID, snap)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// waitForSnapshot polls the snapshot list until one with the given display name
// appears, returning it (or nil on timeout).
func (r *serverSnapshotResource) waitForSnapshot(ctx context.Context, serverID, name string) *gigahost.Snapshot {
	ctx, cancel := context.WithTimeout(ctx, snapshotAppearTimeout)
	defer cancel()

	ticker := time.NewTicker(snapshotPollInterval)
	defer ticker.Stop()

	for {
		snaps, err := r.client.Snapshots.List(ctx, serverID)
		if err == nil {
			for i := range snaps {
				if snaps[i].DisplayName == name {
					return &snaps[i]
				}
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// setSnapshot fills the model from an API snapshot.
// Name is only overwritten when the API returns a non-empty DisplayName; during
// Create the snapshot may still be transitioning and DisplayName is guaranteed
// to match the requested name once it appears (waitForSnapshot checks this).
func (r *serverSnapshotResource) setSnapshot(m *serverSnapshotResourceModel, serverID string, snap *gigahost.Snapshot) {
	snapID := strconv.FormatInt(snap.ID, 10)
	m.SnapshotID = types.StringValue(snapID)
	m.ID = types.StringValue(serverID + "/" + snapID)
	m.State = types.StringValue(string(snap.State))

	if snap.DisplayName != "" {
		m.Name = types.StringValue(snap.DisplayName)
	}
}

// Read refreshes snapshot state; a missing snapshot removes the resource.
func (r *serverSnapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	snaps, err := r.client.Snapshots.List(ctx, state.ServerID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			// Server is gone → snapshot is gone too.
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to list snapshots", err.Error())

		return
	}

	for i := range snaps {
		if strconv.FormatInt(snaps[i].ID, 10) == state.SnapshotID.ValueString() {
			r.setSnapshot(&state, state.ServerID.ValueString(), &snaps[i])

			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

			return
		}
	}

	resp.State.RemoveResource(ctx)
}

// Update is a no-op: every input forces replacement.
func (r *serverSnapshotResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverSnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the snapshot.
func (r *serverSnapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	snapID, err := strconv.ParseInt(state.SnapshotID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid snapshot ID", err.Error())

		return
	}

	if err := r.client.Snapshots.Delete(ctx, state.ServerID.ValueString(), snapID); err != nil {
		resp.Diagnostics.AddError("Failed to delete snapshot", err.Error())

		return
	}
}

// ImportState supports `terraform import ... SERVER_ID/SNAPSHOT_ID`.
func (r *serverSnapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseImportID(req.ID, "server_id", "snapshot_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("snapshot_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
