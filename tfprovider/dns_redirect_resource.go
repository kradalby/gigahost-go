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
	_ resource.Resource                = (*dnsRedirectResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsRedirectResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRedirectResource)(nil)
)

// dnsRedirectResource manages `gigahost_dns_redirect`.
type dnsRedirectResource struct {
	client *gigahost.Client
}

type dnsRedirectModel struct {
	ID        types.String `tfsdk:"id"`
	ZoneID    types.String `tfsdk:"zone_id"`
	Source    types.String `tfsdk:"source"`
	TargetURL types.String `tfsdk:"target_url"`
	Enabled   types.Bool   `tfsdk:"enabled"`
}

// NewDNSRedirectResource is the constructor.
func NewDNSRedirectResource() resource.Resource { return &dnsRedirectResource{} }

// Metadata sets the resource type name.
func (r *dnsRedirectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_redirect"
}

// Schema returns the Terraform schema.
func (r *dnsRedirectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an HTTP redirect for a gigahost.no zone.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `<zone_id>/<source>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "ID of the containing zone.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "Subdomain to redirect (`@` for the zone apex).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target_url": schema.StringAttribute{
				MarkdownDescription: "Target URL including scheme.",
				Required:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the redirect is active.",
				Computed:            true,
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *dnsRedirectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates the redirect.
func (r *dnsRedirectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRedirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.CreateRedirect(ctx, plan.ZoneID.ValueString(), gigahost.CreateRedirectRequest{
		Source:    plan.Source.ValueString(),
		TargetURL: plan.TargetURL.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to create redirect", err.Error())

		return
	}

	plan.ID = types.StringValue(plan.ZoneID.ValueString() + "/" + plan.Source.ValueString())
	plan.Enabled = types.BoolValue(true)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the redirect from the API.
func (r *dnsRedirectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRedirectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	redirects, err := r.client.DNS.ListRedirects(ctx, state.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list redirects", err.Error())

		return
	}

	for _, red := range redirects {
		if red.Source == state.Source.ValueString() {
			state.TargetURL = types.StringValue(red.TargetURL)
			state.Enabled = types.BoolValue(red.Enabled)

			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

			return
		}
	}

	resp.State.RemoveResource(ctx)
}

// Update replaces the target URL on the API.
func (r *dnsRedirectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsRedirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.UpdateRedirect(
		ctx,
		state.ZoneID.ValueString(),
		state.Source.ValueString(),
		plan.TargetURL.ValueString(),
	); err != nil {
		resp.Diagnostics.AddError("Failed to update redirect", err.Error())

		return
	}

	plan.ID = state.ID
	plan.Enabled = state.Enabled

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the redirect.
func (r *dnsRedirectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRedirectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.DeleteRedirect(ctx, state.ZoneID.ValueString(), state.Source.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete redirect", err.Error())

		return
	}
}

// ImportState supports `terraform import ... <ZONE_ID|ZONE_NAME>/SOURCE`.
// SOURCE may be "@" for the zone apex.
func (r *dnsRedirectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseImportID(req.ID, "zone", "source")
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

	source := parts[1]

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source"), source)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID+"/"+source)...)
}
