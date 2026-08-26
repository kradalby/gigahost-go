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
	_ resource.Resource                = (*dnsNameserversResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsNameserversResource)(nil)
	_ resource.ResourceWithImportState = (*dnsNameserversResource)(nil)
)

// dnsNameserversResource manages the set of delegation nameservers for
// a .no domain registered through Gigahost. The canonical use case is
// a customer who keeps their registrar with Gigahost but hosts DNS
// elsewhere (Cloudflare, Route 53, deSEC, etc.).
//
// The API verifies that the submitted nameservers are authoritative
// for the zone before pushing the change through to Norid, and it
// automatically flips the zone's external_dns flag as a side effect.
type dnsNameserversResource struct {
	client *gigahost.Client
}

type dnsNameserversModel struct {
	ID          types.String `tfsdk:"id"`
	ZoneID      types.String `tfsdk:"zone_id"`
	Nameservers types.List   `tfsdk:"nameservers"`
}

// NewDNSNameserversResource constructs the resource.
func NewDNSNameserversResource() resource.Resource { return &dnsNameserversResource{} }

// Metadata sets the resource type name.
func (r *dnsNameserversResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_nameservers"
}

// Schema returns the Terraform schema.
func (r *dnsNameserversResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Delegates a Gigahost-registered domain to an external " +
			"set of nameservers. The API requires at least two nameservers and verifies " +
			"they are authoritative for the zone before pushing the change to Norid. " +
			"Destroying this resource reverts delegation to Gigahost's default " +
			"nameservers.",
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
			"nameservers": schema.ListAttribute{
				MarkdownDescription: "Fully qualified nameserver hostnames (minimum of two).",
				Required:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *dnsNameserversResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create sets the nameservers.
func (r *dnsNameserversResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsNameserversModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ns, diags := listToStrings(ctx, plan.Nameservers)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SetNameservers(ctx, plan.ZoneID.ValueString(), ns); err != nil {
		resp.Diagnostics.AddError("Failed to set nameservers", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes. The API does not expose a GET for nameservers so we
// assume the list in state is authoritative until a configuration
// change or manual drift is detected by Terraform via a later plan.
func (r *dnsNameserversResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

// Update re-applies the nameservers.
func (r *dnsNameserversResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsNameserversModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	ns, diags := listToStrings(ctx, plan.Nameservers)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.SetNameservers(ctx, plan.ZoneID.ValueString(), ns); err != nil {
		resp.Diagnostics.AddError("Failed to update nameservers", err.Error())

		return
	}

	plan.ID = plan.ZoneID

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete reverts delegation to Gigahost's default nameservers.
func (r *dnsNameserversResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsNameserversModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	defaults := []string{
		"ns1.gigahost.no",
		"ns2.gigahost.no",
	}

	if err := r.client.DNS.SetNameservers(ctx, state.ZoneID.ValueString(), defaults); err != nil {
		resp.Diagnostics.AddError("Failed to revert to default nameservers", err.Error())

		return
	}
}

// ImportState imports by zone ID or zone name. The nameservers attribute will
// be null after import because the API has no GET endpoint for nameservers;
// the first apply after import will re-push the configured nameservers.
func (r *dnsNameserversResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, err := resolveZoneIdentifier(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unknown zone in import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID)...)
}
