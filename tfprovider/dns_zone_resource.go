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

// Ensure implementation satisfies the required interfaces.
var (
	_ resource.Resource                = (*dnsZoneResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsZoneResource)(nil)
	_ resource.ResourceWithImportState = (*dnsZoneResource)(nil)
)

// dnsZoneResource manages `gigahost_dns_zone`.
type dnsZoneResource struct {
	client *gigahost.Client
}

// dnsZoneModel is the Terraform schema representation of a DNS zone.
type dnsZoneModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	Active       types.Bool   `tfsdk:"active"`
	Protected    types.Bool   `tfsdk:"protected"`
	IsRegistered types.Bool   `tfsdk:"is_registered"`
	Registrar    types.String `tfsdk:"registrar"`
	ExternalDNS  types.Bool   `tfsdk:"external_dns"`
	RecordCount  types.Int64  `tfsdk:"record_count"`
	UpdatedAt    types.Int64  `tfsdk:"updated_at"`
}

// NewDNSZoneResource is the constructor registered with the provider.
func NewDNSZoneResource() resource.Resource { return &dnsZoneResource{} }

// Metadata sets the resource type name.
func (r *dnsZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

// Schema returns the Terraform schema.
func (r *dnsZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a DNS zone on gigahost.no.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Internal zone ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Zone (domain) name.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Zone type: NATIVE, MASTER or SLAVE. Changing it **replaces** the zone.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					// There is no API to change a zone's type: Update only
					// re-reads it. Without this an in-place change would plan
					// cleanly, do nothing, and then fail the apply.
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"active":        schema.BoolAttribute{MarkdownDescription: "True when the zone is active.", Computed: true},
			"protected":     schema.BoolAttribute{MarkdownDescription: "True when the zone is registered and cannot be deleted via API.", Computed: true},
			"is_registered": schema.BoolAttribute{MarkdownDescription: "True when the domain is registered.", Computed: true},
			"registrar":     schema.StringAttribute{MarkdownDescription: "Domain registrar when registered.", Computed: true},
			"external_dns":  schema.BoolAttribute{MarkdownDescription: "True when the zone uses external nameservers.", Computed: true},
			"record_count":  schema.Int64Attribute{MarkdownDescription: "Number of DNS records in the zone.", Computed: true},
			"updated_at":    schema.Int64Attribute{MarkdownDescription: "Unix timestamp of last zone update.", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client from the provider.
func (r *dnsZoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*gigahost.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("expected *gigahost.Client, got %T", req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create handles the creation of a new DNS zone.
func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.DNS.CreateZone(ctx, gigahost.CreateZoneRequest{
		Name: plan.Name.ValueString(),
		Type: gigahost.ZoneType(plan.Type.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create DNS zone", err.Error())

		return
	}

	// The create endpoint returns only the ID; read back the zone so
	// the state is fully populated.
	plan.ID = types.StringValue(created.ID)

	found, err := r.refreshModel(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read back created zone", err.Error())

		return
	}

	if !found {
		resp.Diagnostics.AddError("DNS zone not found after create", fmt.Sprintf("zone %q not found immediately after creation", created.ID))

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the state from the API.
func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.refreshModel(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())

		return
	}

	if !found {
		// Zone is no longer in the listing; remove it from state so
		// Terraform plans a recreate rather than reporting an error.
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is a no-op at the API level: the zone_name is
// RequiresReplace. Other fields are computed.
func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.refreshModel(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())

		return
	}

	if !found {
		resp.Diagnostics.AddError("DNS zone not found", fmt.Sprintf("zone %q no longer exists", plan.ID.ValueString()))

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the zone from the API.
func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.DeleteZone(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete DNS zone", err.Error())

		return
	}
}

// ImportState allows `terraform import gigahost_dns_zone.x <ZONE_ID|ZONE_NAME>`.
func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, err := resolveZoneIdentifier(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unknown zone in import ID",
			fmt.Sprintf("Could not resolve zone %q: %s", req.ID, err),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID)...)
}

// refreshModel looks up the zone by ID and fills all computed fields in the
// given model. It returns (true, nil) on success, (false, nil) when the zone
// is absent from the listing (the caller decides whether to remove state or
// error), and (false, err) when the listing itself fails.
func (r *dnsZoneResource) refreshModel(ctx context.Context, m *dnsZoneModel) (bool, error) {
	zones, err := r.client.DNS.ListZones(ctx)
	if err != nil {
		return false, err
	}

	for _, z := range zones {
		if z.ID == m.ID.ValueString() {
			// name is Required, so Terraform demands the final state match
			// the config exactly. The API lower-cases zone names, so writing
			// its form back over a config that said "Example.no" fails the
			// apply with "inconsistent result after apply" — after the zone
			// has already been created.
			if !strings.EqualFold(m.Name.ValueString(), z.Name) {
				m.Name = types.StringValue(z.Name)
			}

			m.Type = types.StringValue(string(z.Type))
			m.Active = types.BoolValue(z.Active)
			m.Protected = types.BoolValue(z.Protected)
			m.IsRegistered = types.BoolValue(z.IsRegistered)
			m.Registrar = types.StringValue(z.Registrar)
			m.ExternalDNS = types.BoolValue(z.ExternalDNS)
			m.RecordCount = types.Int64Value(int64(z.RecordCount))
			m.UpdatedAt = unixOrNull(z.UpdatedAt)

			return true, nil
		}
	}

	return false, nil
}
