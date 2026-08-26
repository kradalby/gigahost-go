package tfprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// ptrZoneFacts derives ip_version and CIDR prefix from an arpa zone name.
//
//	"168.125.185.in-addr.arpa" -> ("ipv4", "185.125.168.0/24", nil)
//	ip6.arpa nibble names -> ("ipv6", "<prefix>/<bits>", nil)
//
// The returned prefix uses CIDR notation and matches what Create stores in
// state, ensuring import convergence.
func ptrZoneFacts(arpaName string) (string, string, error) {
	if arpaName == "" {
		return "", "", errors.New("ptrZoneFacts: empty zone name")
	}

	// lower is used only for suffix routing; arpaName (original case) is passed
	// to the per-family helpers so IPv6 nibble extraction is never lowercased.
	lower := strings.ToLower(arpaName)

	switch {
	case strings.HasSuffix(lower, ".in-addr.arpa"):
		return ptrZoneFactsIPv4(arpaName)
	case strings.HasSuffix(lower, ".ip6.arpa"):
		return ptrZoneFactsIPv6(arpaName)
	default:
		return "", "", fmt.Errorf("ptrZoneFacts: %q is not an in-addr.arpa or ip6.arpa zone", arpaName)
	}
}

// ptrZoneFactsIPv4 handles in-addr.arpa zones.
// The octets before .in-addr.arpa are reversed and padded to 4 octets.
func ptrZoneFactsIPv4(arpaName string) (string, string, error) {
	const suffix = ".in-addr.arpa"

	body := arpaName[:len(arpaName)-len(suffix)]
	if body == "" {
		return "", "", fmt.Errorf("ptrZoneFacts: %q has no octets before .in-addr.arpa", arpaName)
	}

	parts := strings.Split(body, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return "", "", fmt.Errorf("ptrZoneFacts: %q: expected 1–3 octets, got %d", arpaName, len(parts))
	}

	bits := len(parts) * 8

	// Reverse the octets and pad to 4 with zeros.
	rev := make([]string, 4)
	for i := range rev {
		rev[i] = "0"
	}

	for i, p := range parts {
		rev[len(parts)-1-i] = p
	}

	cidr := fmt.Sprintf("%s/%d", strings.Join(rev, "."), bits)

	// Validate the CIDR.
	if _, _, parseErr := net.ParseCIDR(cidr); parseErr != nil {
		return "", "", fmt.Errorf("ptrZoneFacts: derived CIDR %q is invalid: %w", cidr, parseErr)
	}

	return "ipv4", cidr, nil
}

// ptrZoneFactsIPv6 handles ip6.arpa nibble-reversed zones.
// Nibbles before .ip6.arpa are reversed and assembled into the prefix.
func ptrZoneFactsIPv6(arpaName string) (string, string, error) {
	const suffix = ".ip6.arpa"

	body := arpaName[:len(arpaName)-len(suffix)]
	if body == "" {
		return "", "", fmt.Errorf("ptrZoneFacts: %q has no nibbles before .ip6.arpa", arpaName)
	}

	nibbles := strings.Split(body, ".")
	if len(nibbles) == 0 || len(nibbles) > 32 {
		return "", "", fmt.Errorf("ptrZoneFacts: %q: expected 1–32 nibbles, got %d", arpaName, len(nibbles))
	}

	bits := len(nibbles) * 4

	// Reverse the nibble list and pad to 32 with zeros.
	rev := make([]string, 32)
	for i := range rev {
		rev[i] = "0"
	}

	for i, n := range nibbles {
		rev[len(nibbles)-1-i] = n
	}

	// Group into 16-bit groups (4 nibbles each) separated by colons.
	groups := make([]string, 8)
	for i := range groups {
		groups[i] = strings.Join(rev[i*4:(i+1)*4], "")
	}

	rawAddr := strings.Join(groups, ":")
	ip := net.ParseIP(rawAddr)

	if ip == nil {
		return "", "", fmt.Errorf("ptrZoneFacts: derived IPv6 address %q is invalid", rawAddr)
	}

	cidr := fmt.Sprintf("%s/%d", ip.String(), bits)

	// Use net.ParseCIDR for canonical form.
	_, ipNet, parseErr := net.ParseCIDR(cidr)
	if parseErr != nil {
		return "", "", fmt.Errorf("ptrZoneFacts: derived CIDR %q is invalid: %w", cidr, parseErr)
	}

	// Build prefix: address + / + bits (use the network address with correct bits).
	addr := ipNet.IP.String()
	result := fmt.Sprintf("%s/%d", addr, bits)

	return "ipv6", result, nil
}

var (
	_ resource.Resource                = (*dnsPTRZoneResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsPTRZoneResource)(nil)
	_ resource.ResourceWithImportState = (*dnsPTRZoneResource)(nil)
)

// dnsPTRZoneResource manages reverse DNS (PTR) zones.
type dnsPTRZoneResource struct {
	client *gigahost.Client
}

type dnsPTRZoneModel struct {
	ID        types.String   `tfsdk:"id"`
	Prefix    ptrPrefixValue `tfsdk:"prefix"`
	IPVersion types.String   `tfsdk:"ip_version"`
	ZoneName  types.String   `tfsdk:"zone_name"`
}

// NewDNSPTRZoneResource constructs the resource.
func NewDNSPTRZoneResource() resource.Resource { return &dnsPTRZoneResource{} }

// Metadata sets the resource type name.
func (r *dnsPTRZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_ptr_zone"
}

// Schema returns the Terraform schema.
func (r *dnsPTRZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A reverse DNS (PTR) zone owned by the account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Generated zone ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "IP prefix in bare or CIDR form. Accepted: `185.181.63` or `185.181.63.0/24`; `2a03:94e0::` or `2a03:94e0::/32`. The canonical CIDR form is stored in state; bare and CIDR forms are treated as equivalent.",
				Required:            true,
				CustomType:          ptrPrefixType{},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ip_version": schema.StringAttribute{
				MarkdownDescription: "`ipv4` or `ipv6`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"zone_name": schema.StringAttribute{
				MarkdownDescription: "PTR zone name, e.g. `63.181.185.in-addr.arpa` or " +
					"`0.e.4.9.3.0.a.2.ip6.arpa`.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *dnsPTRZoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates the PTR zone.
func (r *dnsPTRZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsPTRZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.DNS.CreatePTRZone(ctx, gigahost.CreatePTRZoneRequest{
		Prefix:    plan.Prefix.ValueString(),
		IPVersion: gigahost.IPVersion(plan.IPVersion.ValueString()),
		ZoneName:  plan.ZoneName.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create PTR zone", err.Error())

		return
	}

	plan.ID = types.StringValue(created.ZoneID)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes PTR zone state via ListZones.
func (r *dnsPTRZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsPTRZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	zones, err := r.client.DNS.ListZones(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list zones", err.Error())

		return
	}

	zoneID := state.ID.ValueString()

	var found *gigahost.Zone

	for i := range zones {
		if zones[i].ID == zoneID {
			found = &zones[i]

			break
		}
	}

	if found == nil {
		// Zone deleted out-of-band.
		resp.State.RemoveResource(ctx)

		return
	}

	state.ZoneName = types.StringValue(found.Name)

	ipVer, pfx, factErr := ptrZoneFacts(found.Name)
	if factErr != nil {
		resp.Diagnostics.AddError("Failed to derive PTR zone facts from zone name", factErr.Error())

		return
	}

	state.IPVersion = types.StringValue(ipVer)
	state.Prefix = ptrPrefixValue{StringValue: types.StringValue(pfx)}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update would rebuild the zone; all attributes are RequiresReplace.
func (r *dnsPTRZoneResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

// Delete removes the PTR zone via the standard zone-delete endpoint.
func (r *dnsPTRZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsPTRZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DNS.DeleteZone(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete PTR zone", err.Error())

		return
	}
}

// ImportState imports by zone ID or arpa zone name. PTR zones appear in
// ListZones, so resolveZoneIdentifier works for both.
func (r *dnsPTRZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, err := resolveZoneIdentifier(ctx, r.client, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unknown zone in import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), zoneID)...)
}
