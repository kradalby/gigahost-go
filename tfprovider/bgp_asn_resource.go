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
	_ resource.Resource                = (*bgpASNResource)(nil)
	_ resource.ResourceWithConfigure   = (*bgpASNResource)(nil)
	_ resource.ResourceWithImportState = (*bgpASNResource)(nil)
)

// bgpASNResource submits an ASN to Gigahost for BGP peering approval.
// Submission is asynchronous — the resource reports the approval status
// via the `status` attribute; Read refreshes it each plan.
type bgpASNResource struct {
	client *gigahost.Client
}

type bgpASNModel struct {
	ID      types.String `tfsdk:"id"`
	ASN     asnValue     `tfsdk:"asn"`
	Name    types.String `tfsdk:"name"`
	Status  types.String `tfsdk:"status"`
	Country types.String `tfsdk:"country"`
}

// NewBGPASNResource constructs the resource.
func NewBGPASNResource() resource.Resource { return &bgpASNResource{} }

// Metadata sets the resource type name.
func (r *bgpASNResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bgp_asn"
}

// Schema returns the Terraform schema.
func (r *bgpASNResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Submits an ASN for BGP peering approval. Approval is " +
			"performed out-of-band by Gigahost; the `status` field reflects progress. " +
			"`terraform destroy` is a no-op — ASN withdrawal requires support intervention.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Internal ASN record ID used by downstream sessions.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"asn": schema.StringAttribute{
				MarkdownDescription: "ASN, e.g. `212345` or `AS212345`. AS-prefix and case are normalised; `AS212345`, `as212345` and `212345` are all equivalent.",
				Required:            true,
				CustomType:          asnType{},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name":    schema.StringAttribute{MarkdownDescription: "Human-readable ASN name.", Computed: true},
			"status":  schema.StringAttribute{MarkdownDescription: "`pending`, `active` or `rejected`.", Computed: true},
			"country": schema.StringAttribute{MarkdownDescription: "ASN country.", Computed: true},
		},
	}
}

// Configure receives the shared Gigahost client.
func (r *bgpASNResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create submits the ASN.
func (r *bgpASNResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bgpASNModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.BGP.SubmitASN(ctx, plan.ASN.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to submit ASN", err.Error())

		return
	}

	// Look up the newly created record.
	if _, err := r.refreshModel(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to look up ASN after submission", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes ASN state from the API.
func (r *bgpASNResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bgpASNModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.refreshModel(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read ASN", err.Error())

		return
	}

	if !found {
		// ASN absent from /bgp listing — definitively gone.
		resp.State.RemoveResource(ctx)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is unreachable — all attributes are RequiresReplace.
func (r *bgpASNResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

// Delete is a no-op: the API has no ASN withdrawal. The resource is
// removed from state but the record remains in Gigahost's database.
func (r *bgpASNResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// ImportState imports by ASN number; accepts bare numeric ("212345") or
// AS-prefixed ("AS212345") forms.
func (r *bgpASNResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	asn, err := normalizeASNImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("asn"), asnValue{StringValue: types.StringValue(asn)})...)
}

// refreshModel fetches /bgp and populates m with the matching ASN record.
// It returns (true, nil) when found, (false, nil) when the ASN is absent from
// the listing (definitively removed), and (false, err) for transport failures.
func (r *bgpASNResource) refreshModel(ctx context.Context, m *bgpASNModel) (bool, error) {
	data, err := r.client.BGP.Get(ctx)
	if err != nil {
		return false, err
	}

	// Normalize the state ASN the same way SubmitASN does: strip any AS prefix
	// so a config of "AS212345" matches the API's bare "212345".
	raw := m.ASN.ValueString()

	// If normalization fails the ASN in state is unparseable; treat as absent
	// so bad state can be reconciled by re-importing.
	wanted, _ := normalizeASNImportID(raw)
	if wanted == "" {
		return false, nil
	}

	for _, a := range data.ASNs {
		if a.ASN == wanted {
			m.ID = types.StringValue(a.ID)
			m.ASN = asnValue{StringValue: types.StringValue(a.ASN)}
			m.Name = types.StringValue(a.Name)
			m.Status = types.StringValue(a.Status)
			m.Country = types.StringValue(a.Country)

			return true, nil
		}
	}

	return false, nil
}
