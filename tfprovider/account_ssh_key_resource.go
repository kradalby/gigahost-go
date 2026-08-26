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
	_ resource.Resource                = (*accountSSHKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*accountSSHKeyResource)(nil)
	_ resource.ResourceWithImportState = (*accountSSHKeyResource)(nil)
)

// accountSSHKeyResource manages an SSH public key on the Gigahost account.
// Because the add endpoint returns no key ID, the resource looks up the
// newly created key by name immediately after creation. Name must therefore
// be unique among SSH keys on the account.
type accountSSHKeyResource struct {
	client *gigahost.Client
}

type accountSSHKeyResourceModel struct {
	ID        types.String      `tfsdk:"id"`
	Name      types.String      `tfsdk:"name"`
	PublicKey sshPublicKeyValue `tfsdk:"public_key"`
	AddedAt   types.Int64       `tfsdk:"added_at"`
}

// NewAccountSSHKeyResource constructs the resource.
func NewAccountSSHKeyResource() resource.Resource { return &accountSSHKeyResource{} }

// Metadata sets the resource type name.
func (r *accountSSHKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account_ssh_key"
}

// Schema returns the Terraform schema.
func (r *accountSSHKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds an SSH public key to the Gigahost account. " +
			"The key name must be unique on the account because the API does not " +
			"return the new key ID at creation time — the resource identifies the " +
			"key by name. Destroying the resource deletes the key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Gigahost's internal key ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable key label. Must be unique on the account.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "OpenSSH-format public key (e.g. `ssh-ed25519 AAAA…`). " +
					"Values are compared ignoring surrounding whitespace, so a trailing " +
					"newline from `file(\"key.pub\")` does not produce a perpetual diff.",
				Required:      true,
				CustomType:    sshPublicKeyType{},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"added_at": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp of when the key was added.",
				Computed:            true,
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *accountSSHKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create adds the SSH key and looks it up by name to obtain its ID.
func (r *accountSSHKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accountSSHKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Account.AddSSHKey(ctx, plan.Name.ValueString(), plan.PublicKey.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to add SSH key", err.Error())

		return
	}

	// The add endpoint returns no key ID; look up by name.
	acc, err := r.client.Account.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read account after adding SSH key", err.Error())

		return
	}

	wantedName := plan.Name.ValueString()

	var (
		foundID      string
		foundAddedAt int64
	)

	for _, k := range acc.SSHKeys {
		if k.Name == wantedName {
			if foundID == "" || k.AddedAt.Unix() > foundAddedAt {
				foundID = k.ID
				foundAddedAt = k.AddedAt.Unix()
			}
		}
	}

	if foundID == "" {
		resp.Diagnostics.AddError("SSH key not found after creation",
			fmt.Sprintf("key with name %q not found in account SSH keys", wantedName))

		return
	}

	plan.ID = types.StringValue(foundID)
	plan.AddedAt = types.Int64Value(foundAddedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the SSH key state.
func (r *accountSSHKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accountSSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	acc, err := r.client.Account.Get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read account", err.Error())

		return
	}

	wantedID := state.ID.ValueString()

	for _, k := range acc.SSHKeys {
		if k.ID == wantedID {
			state.Name = types.StringValue(k.Name)
			state.AddedAt = unixOrNull(k.AddedAt)
			// Write the API's key form, trimmed to a canonical shape so
			// ImportStateVerify (a literal compare) is robust against the API
			// storing a trailing newline. Semantic equality on
			// sshPublicKeyValue additionally means whitespace-only differences
			// (e.g. a trailing newline from file("key.pub")) never surface as
			// a plan diff.
			state.PublicKey = sshPublicKeyValue{StringValue: types.StringValue(strings.TrimSpace(k.Data))}

			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

			return
		}
	}

	// Key no longer exists on the account.
	resp.State.RemoveResource(ctx)
}

// Update is unreachable — both mutable attributes are RequiresReplace.
func (r *accountSSHKeyResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

// Delete removes the SSH key from the account.
func (r *accountSSHKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accountSSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Account.DeleteSSHKey(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete SSH key", err.Error())
	}
}

// ImportState imports by key ID.
func (r *accountSSHKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
