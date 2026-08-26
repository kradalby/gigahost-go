package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*accountAPIKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*accountAPIKeyResource)(nil)
	_ resource.ResourceWithImportState = (*accountAPIKeyResource)(nil)
)

// accountAPIKeyResource manages a personal API key on the Gigahost account.
// The secret is returned once at creation and stored in state; it is never
// surfaced again by the API.
type accountAPIKeyResource struct {
	client *gigahost.Client
}

// apiKeyPermModel is the Terraform model for a single permission entry.
type apiKeyPermModel struct {
	Mode types.String `tfsdk:"mode"`
	All  types.Bool   `tfsdk:"all"`
	IDs  types.List   `tfsdk:"ids"`
}

// apiKeyPermsModel is the Terraform model for the full permissions set.
type apiKeyPermsModel struct {
	DNS        *apiKeyPermModel `tfsdk:"dns"`
	Servers    *apiKeyPermModel `tfsdk:"servers"`
	Webhosting *apiKeyPermModel `tfsdk:"webhosting"`
	Racks      *apiKeyPermModel `tfsdk:"racks"`
	Support    *apiKeyPermModel `tfsdk:"support"`
	Billing    *apiKeyPermModel `tfsdk:"billing"`
	Account    *apiKeyPermModel `tfsdk:"account"`
}

type accountAPIKeyModel struct {
	ID          types.String      `tfsdk:"id"`
	Label       types.String      `tfsdk:"label"`
	Prefix      types.String      `tfsdk:"prefix"`
	Secret      types.String      `tfsdk:"secret"`
	Status      types.String      `tfsdk:"status"`
	ContactID   types.String      `tfsdk:"contact_id"`
	CreatedAt   types.Int64       `tfsdk:"created_at"`
	ExpiresAt   types.Int64       `tfsdk:"expires_at"`
	Permissions *apiKeyPermsModel `tfsdk:"permissions"`
}

// NewAccountAPIKeyResource constructs the resource.
func NewAccountAPIKeyResource() resource.Resource { return &accountAPIKeyResource{} }

// Metadata sets the resource type name.
func (r *accountAPIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account_api_key"
}

// permSchema returns the nested schema for one permission category block.
// The category name is taken so each renders with its own description rather
// than an empty cell on the registry page.
func permSchema(category string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Access this key grants to " + category + ".",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "`r` for read-only or `rw` for read-write.",
				Required:            true,
			},
			"all": schema.BoolAttribute{
				MarkdownDescription: "When `true`, the permission applies to all resources of this category.",
				Required:            true,
			},
			"ids": schema.ListAttribute{
				MarkdownDescription: "Explicit resource IDs the permission applies to when `all` is `false`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Schema returns the Terraform schema.
func (r *accountAPIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a personal API key on the Gigahost account. " +
			"The secret is returned once at creation and stored in state as a sensitive " +
			"value; it cannot be recovered later. Destroying the resource revokes the key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Gigahost's internal key ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Human-readable label for the key.",
				Required:            true,
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "Non-secret prefix of the key (safe to display).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret": schema.StringAttribute{
				MarkdownDescription: "The full API key secret. Shown once at creation; store it immediately.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Key status: `active` or `revoked`.",
				Computed:            true,
			},
			"contact_id": schema.StringAttribute{
				MarkdownDescription: "ID of the contact that owns the key.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp of when the key was created.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"expires_at": schema.Int64Attribute{
				MarkdownDescription: "Unix timestamp of key expiry. `0` or omitted means no expiry. " +
					"Changing this value forces a new key to be created.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()},
			},
			"permissions": schema.SingleNestedAttribute{
				MarkdownDescription: "Per-category access permissions. Omitting a category grants no access to it.",
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"dns":        permSchema("DNS zones and records"),
					"servers":    permSchema("servers"),
					"webhosting": permSchema("web hosting"),
					"racks":      permSchema("rack colocation"),
					"support":    permSchema("support tickets"),
					"billing":    permSchema("billing and invoices"),
					"account":    permSchema("account settings"),
				},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *accountAPIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates the API key.
func (r *accountAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accountAPIKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	perms, diags := modelToPerms(ctx, plan.Permissions)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	createReq := gigahost.CreateAPIKeyRequest{
		Label:       plan.Label.ValueString(),
		Permissions: perms,
	}

	if !plan.ExpiresAt.IsNull() && !plan.ExpiresAt.IsUnknown() && plan.ExpiresAt.ValueInt64() != 0 {
		v := plan.ExpiresAt.ValueInt64()
		createReq.ExpiresAt = &v
	}

	created, err := r.client.Account.CreateAPIKey(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create API key", err.Error())

		return
	}

	// Retrieve full key details for computed fields.
	key, err := r.client.Account.ListAPIKeys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list API keys after creation", err.Error())

		return
	}

	var found *gigahost.APIKey

	for i := range key {
		if key[i].Prefix == created.Prefix {
			found = &key[i]

			break
		}
	}

	if found == nil {
		resp.Diagnostics.AddError("API key not found after creation",
			fmt.Sprintf("key with prefix %q not found", created.Prefix))

		return
	}

	state, diags := apiKeyToModel(ctx, found)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// The secret is only available in the create response.
	state.Secret = types.StringValue(created.Secret)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Read refreshes the API key state. The secret is preserved from existing state.
func (r *accountAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accountAPIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.Account.GetAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		if gigahost.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to read API key", err.Error())

		return
	}

	updated, diags := apiKeyToModel(ctx, key)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Preserve the secret from prior state — the API never returns it again.
	updated.Secret = state.Secret

	resp.Diagnostics.Append(resp.State.Set(ctx, updated)...)
}

// Update applies label and/or permission changes.
func (r *accountAPIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan accountAPIKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var state accountAPIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	perms, diags := modelToPerms(ctx, plan.Permissions)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := gigahost.UpdateAPIKeyRequest{
		Label:       plan.Label.ValueString(),
		Permissions: &perms,
	}

	if err := r.client.Account.UpdateAPIKey(ctx, state.ID.ValueString(), updateReq); err != nil {
		resp.Diagnostics.AddError("Failed to update API key", err.Error())

		return
	}

	key, err := r.client.Account.GetAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read API key after update", err.Error())

		return
	}

	updated, diags := apiKeyToModel(ctx, key)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Preserve the secret from prior state.
	updated.Secret = state.Secret

	resp.Diagnostics.Append(resp.State.Set(ctx, updated)...)
}

// Delete revokes the API key.
func (r *accountAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accountAPIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Account.DeleteAPIKey(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete API key", err.Error())
	}
}

// ImportState imports by key ID.
func (r *accountAPIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// apiKeyToModel converts a client APIKey to the Terraform model.
func apiKeyToModel(ctx context.Context, k *gigahost.APIKey) (accountAPIKeyModel, diag.Diagnostics) {
	permsModel, diags := permsToModel(ctx, k.Permissions)
	if diags.HasError() {
		return accountAPIKeyModel{}, diags
	}

	m := accountAPIKeyModel{
		ID:        types.StringValue(k.ID),
		Label:     types.StringValue(k.Label),
		Prefix:    types.StringValue(k.Prefix),
		Secret:    types.StringNull(), // never returned by read endpoints
		Status:    types.StringValue(k.Status),
		ContactID: types.StringValue(k.ContactID),
		CreatedAt: unixOrNull(k.CreatedAt),
		// null, not 0: "no expiry" and "expires at the epoch" are different
		// things, and RequiresReplace sits on top of this attribute.
		ExpiresAt:   types.Int64Null(),
		Permissions: permsModel,
	}

	if k.ExpiresAt != nil {
		m.ExpiresAt = unixOrNull(*k.ExpiresAt)
	}

	return m, diags
}

// permsToModel converts client APIKeyPermissions to the Terraform model.
func permsToModel(ctx context.Context, p gigahost.APIKeyPermissions) (*apiKeyPermsModel, diag.Diagnostics) {
	var allDiags diag.Diagnostics

	dns, diags := permToModel(ctx, p.DNS)
	allDiags = append(allDiags, diags...)

	servers, diags := permToModel(ctx, p.Servers)
	allDiags = append(allDiags, diags...)

	webhosting, diags := permToModel(ctx, p.Webhosting)
	allDiags = append(allDiags, diags...)

	racks, diags := permToModel(ctx, p.Racks)
	allDiags = append(allDiags, diags...)

	support, diags := permToModel(ctx, p.Support)
	allDiags = append(allDiags, diags...)

	billing, diags := permToModel(ctx, p.Billing)
	allDiags = append(allDiags, diags...)

	account, diags := permToModel(ctx, p.Account)
	allDiags = append(allDiags, diags...)

	if allDiags.HasError() {
		return nil, allDiags
	}

	return &apiKeyPermsModel{
		DNS:        dns,
		Servers:    servers,
		Webhosting: webhosting,
		Racks:      racks,
		Support:    support,
		Billing:    billing,
		Account:    account,
	}, allDiags
}

// permToModel converts a single client APIKeyPermission to the Terraform model.
func permToModel(ctx context.Context, p *gigahost.APIKeyPermission) (*apiKeyPermModel, diag.Diagnostics) {
	if p == nil {
		return nil, nil
	}

	ids := p.IDs
	if ids == nil {
		ids = []string{}
	}

	idsList, diags := types.ListValueFrom(ctx, types.StringType, ids)
	if diags.HasError() {
		return nil, diags
	}

	return &apiKeyPermModel{
		Mode: types.StringValue(p.Mode),
		All:  types.BoolValue(p.All),
		IDs:  idsList,
	}, nil
}

// modelToPerms converts the Terraform permissions model to the client type.
func modelToPerms(ctx context.Context, m *apiKeyPermsModel) (gigahost.APIKeyPermissions, diag.Diagnostics) {
	if m == nil {
		return gigahost.APIKeyPermissions{}, nil
	}

	var allDiags diag.Diagnostics

	dns, diags := modelToPerm(ctx, m.DNS)
	allDiags = append(allDiags, diags...)

	servers, diags := modelToPerm(ctx, m.Servers)
	allDiags = append(allDiags, diags...)

	webhosting, diags := modelToPerm(ctx, m.Webhosting)
	allDiags = append(allDiags, diags...)

	racks, diags := modelToPerm(ctx, m.Racks)
	allDiags = append(allDiags, diags...)

	support, diags := modelToPerm(ctx, m.Support)
	allDiags = append(allDiags, diags...)

	billing, diags := modelToPerm(ctx, m.Billing)
	allDiags = append(allDiags, diags...)

	account, diags := modelToPerm(ctx, m.Account)
	allDiags = append(allDiags, diags...)

	return gigahost.APIKeyPermissions{
		DNS:        dns,
		Servers:    servers,
		Webhosting: webhosting,
		Racks:      racks,
		Support:    support,
		Billing:    billing,
		Account:    account,
	}, allDiags
}

// modelToPerm converts a single Terraform permission model to the client type.
func modelToPerm(ctx context.Context, m *apiKeyPermModel) (*gigahost.APIKeyPermission, diag.Diagnostics) {
	if m == nil {
		return nil, nil
	}

	ids, diags := listToStrings(ctx, m.IDs)
	if diags.HasError() {
		return nil, diags
	}

	return &gigahost.APIKeyPermission{
		Mode: m.Mode.ValueString(),
		All:  m.All.ValueBool(),
		IDs:  ids,
	}, nil
}
