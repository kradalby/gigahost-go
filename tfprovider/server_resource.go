package tfprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

var (
	_ resource.Resource                = (*serverResource)(nil)
	_ resource.ResourceWithConfigure   = (*serverResource)(nil)
	_ resource.ResourceWithImportState = (*serverResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*serverResource)(nil)
)

// deployReadyTimeout caps how long Create waits for provisioning to finish
// when no timeouts block overrides it.
const deployReadyTimeout = 15 * time.Minute

// reinstallTimeout caps how long an in-place reinstall waits to settle.
const reinstallTimeout = 15 * time.Minute

// Deploy wait tuning. The /deploy/status view is a live projection: it only
// lists an order while its server is provisioning and has no failure status,
// so an order can vanish without signal. Once a server id has appeared there,
// the durable /servers record (keyed by that id, since /servers carries no
// order linkage) is the completion source.
const (
	deployPollInterval = 10 * time.Second
	// listFallbackEvery: after this many consecutive status-misses, confirm
	// completion via the server record instead of the status view.
	listFallbackEvery = 3
	// maxStatusPollErrors: consecutive /deploy/status errors tolerated before
	// the wait gives up (transient API blips are common during provisioning).
	maxStatusPollErrors = 4
	// maxGoneChecks: a previously-seen server absent from the server record
	// this many fallback checks in a row is treated as a failed deploy.
	maxGoneChecks = 20
)

// Read absence confirmation. The /servers list transiently omits a live server
// for tens of seconds (observed live), so a single miss must not delete a
// billed server from state.
const (
	serverAbsenceReads = 5
	serverAbsenceDelay = 15 * time.Second
)

// serverResource deploys and manages an hourly-billed cloud server via the
// /deploy endpoints. Selection is by slug — type/size/region/os — resolved
// against the live catalog at create time; no catalog IDs appear in
// configuration. Every input except os is immutable and forces a new
// server. Changing os between two OS slugs reinstalls the server in place
// (same ID and IP, disk wiped); transitions involving iso or rescue still
// replace. Destroying the resource cancels the server (stopping billing).
type serverResource struct {
	client *gigahost.Client

	// pollInterval and absenceDelay pace the deploy wait and the Read
	// absence-confirmation loops. Zero means use the package defaults; tests
	// shrink them so the loops run instantly.
	pollInterval time.Duration
	absenceDelay time.Duration
}

func (r *serverResource) poll() time.Duration {
	if r.pollInterval > 0 {
		return r.pollInterval
	}

	return deployPollInterval
}

func (r *serverResource) absence() time.Duration {
	if r.absenceDelay > 0 {
		return r.absenceDelay
	}

	return serverAbsenceDelay
}

type serverResourceModel struct {
	ID       types.String `tfsdk:"id"`
	OrderID  types.String `tfsdk:"order_id"`
	Platform types.String `tfsdk:"platform"`
	Type     types.String `tfsdk:"type"`
	Size     types.String `tfsdk:"size"`
	Region   types.String `tfsdk:"region"`
	OS       types.String `tfsdk:"os"`
	ISO      types.String `tfsdk:"iso"`
	Rescue   types.Bool   `tfsdk:"rescue"`
	Hostname types.String `tfsdk:"hostname"`
	SSHKeys  types.List   `tfsdk:"ssh_keys"`
	Backups  types.Bool   `tfsdk:"backups"`

	IP          types.String  `tfsdk:"ip"`
	IPv6        types.String  `tfsdk:"ipv6"`
	Password    types.String  `tfsdk:"password"`
	Status      types.String  `tfsdk:"status"`
	Cores       types.Int64   `tfsdk:"cores"`
	MemoryGB    types.Int64   `tfsdk:"memory_gb"`
	StorageGB   types.Int64   `tfsdk:"storage_gb"`
	RateHourly  types.Float64 `tfsdk:"rate_hourly"`
	RateMonthly types.Float64 `tfsdk:"rate_monthly"`
	PrimaryIPID types.String  `tfsdk:"primary_ip_id"`

	// Read-only runtime facts mirrored from the live server record.
	Location  types.String `tfsdk:"location"`
	VPSType   types.String `tfsdk:"vps_type"`
	Suspended types.Bool   `tfsdk:"suspended"`
	Bandwidth types.Int64  `tfsdk:"bandwidth"`
	CreatedAt types.Int64  `tfsdk:"created_at"`
	OSName    types.String `tfsdk:"os_name"`
	OSRelease types.String `tfsdk:"os_release"`

	// IPs is a types.List (not a []serverIPModel) because this Computed
	// attribute is unknown during the create plan, and a plain slice cannot
	// represent an unknown value.
	IPs types.List `tfsdk:"ips"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// serverIPObjectType is the element type of the server resource's ips list.
func serverIPObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":        types.StringType,
		"subnet_id": types.StringType,
		"version":   types.StringType,
		"address":   types.StringType,
		"reverse":   types.StringType,
		"type":      types.StringType,
	}}
}

// NewServerResource constructs the resource.
func NewServerResource() resource.Resource { return &serverResource{} }

// Metadata sets the resource type name.
func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

// slugValidator rejects values that cannot possibly be slugs (whitespace,
// uppercase) at plan time, so typos fail `validate` instead of apply.
// Membership is checked against the live catalog at apply — valid values
// are never hardcoded here.
type slugValidator struct{ attr string }

func (v slugValidator) Description(context.Context) string {
	return "must be a lowercase slug without spaces"
}

func (v slugValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v slugValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	s := req.ConfigValue.ValueString()
	if strings.ContainsAny(s, " \t\n") || s != strings.ToLower(s) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid "+v.attr+" slug",
			fmt.Sprintf("%q is not a slug: use the lowercase dashed form, e.g. from `gigahost deploy %ss`", s, v.attr),
		)
	}
}

// requiresReplaceUnlessAdopting fires RequiresReplace only when the prior
// state value is known and non-null; a null prior value means the resource was
// just imported and the config value is being adopted, not changed.
//
// It wraps the framework's RequiresReplaceIf, which already skips create
// (null prior state object), destroy (null plan), and equal plan/state. Two
// extra guards refine it:
//
//   - State null → adoption (null -> "b"): apply in place, never replace; the
//     adoption branch in Update writes the config value into state.
//   - Plan unknown → a Computed attribute (region) whose config was omitted is
//     about to be filled from prior state by a following UseStateForUnknown
//     modifier; an unknown plan is never a user-driven change, so it must not
//     force replacement.
//
// What remains ("a" -> "b" with both sides known) is a genuine change and
// replaces.
const adoptingReplaceDesc = "replaces the server when changed, except when adopting a value into freshly imported state"

func requiresReplaceUnlessAdoptingStr() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsUnknown()
		},
		adoptingReplaceDesc, adoptingReplaceDesc,
	)
}

func requiresReplaceUnlessAdoptingBool() planmodifier.Bool {
	return boolplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsUnknown()
		},
		adoptingReplaceDesc, adoptingReplaceDesc,
	)
}

func requiresReplaceUnlessAdoptingList() planmodifier.List {
	return listplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsUnknown()
		},
		adoptingReplaceDesc, adoptingReplaceDesc,
	)
}

// Schema returns the Terraform schema.
func (r *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplaceStr := []planmodifier.String{requiresReplaceUnlessAdoptingStr()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Deploys an hourly-billed cloud server, selected by slugs: `type` + " +
			"`size` (+ optional `region`) and one of `os`, `iso`, or `rescue`. Slugs resolve " +
			"against the live catalog at create time — list them with `gigahost deploy " +
			"types|sizes|regions|os` or the `gigahost_server_size`/`gigahost_operating_system` " +
			"data sources. Changing `os` **reinstalls the server in place** (same ID and IP, " +
			"**disk wiped**, SSH keys not re-injected); every other input change replaces the " +
			"server. Destroying the resource cancels the server and stops billing.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"order_id": schema.StringAttribute{
				MarkdownDescription: "Deploy order ID. Recorded the moment the order is placed so a " +
					"deploy that fails mid-provisioning can still be cancelled by `terraform destroy`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"platform": schema.StringAttribute{
				MarkdownDescription: "Platform: `cloud` (default). `metal` exists in the catalog " +
					"but is not yet deployable through this resource.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(gigahost.PlatformCloud),
				Validators:    []validator.String{slugValidator{attr: "platform"}},
				PlanModifiers: requiresReplaceStr,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Server type slug, e.g. `value` or `performance`. List with " +
					"`gigahost deploy types`. Changing it **replaces** the server.",
				Required:      true,
				Validators:    []validator.String{slugValidator{attr: "type"}},
				PlanModifiers: requiresReplaceStr,
			},
			"size": schema.StringAttribute{
				MarkdownDescription: "Size slug, e.g. `2c-4gb-40gb` (cores-memory-disk). List with " +
					"`gigahost deploy sizes` or pick via the `gigahost_server_size` data source. " +
					"Changing it **replaces** the server.",
				Required:      true,
				Validators:    []validator.String{slugValidator{attr: "size"}},
				PlanModifiers: requiresReplaceStr,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region slug, e.g. `sfj` (list with `gigahost deploy regions`). " +
					"Optional while the chosen size is offered in exactly one region — that region " +
					"is used automatically and recorded here.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{slugValidator{attr: "region"}},
				PlanModifiers: []planmodifier.String{
					requiresReplaceUnlessAdoptingStr(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"os": schema.StringAttribute{
				MarkdownDescription: "Operating system slug, e.g. `debian-12` (list with `gigahost " +
					"deploy os`; codenames like `bookworm` also resolve). Exactly one of `os`, " +
					"`iso`, or `rescue`. Changing this between two OS slugs **reinstalls the server " +
					"in place**: the ID and IP are kept, but **the disk is wiped** and SSH keys are " +
					"not re-injected. Transitions involving `iso` or `rescue` replace the server.",
				Optional: true,
				// Replacement vs in-place reinstall is decided in ModifyPlan.
			},
			"iso": schema.StringAttribute{
				MarkdownDescription: "Uploaded ISO to boot, by name (list with `gigahost deploy " +
					"isos`). Exactly one of `os`, `iso`, or `rescue`.",
				Optional:      true,
				PlanModifiers: requiresReplaceStr,
			},
			"rescue": schema.BoolAttribute{
				MarkdownDescription: "Boot into rescue mode. Exactly one of `os`, `iso`, or `rescue`.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Bool{requiresReplaceUnlessAdoptingBool()},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "Hostname for the new server.",
				Optional:            true,
				PlanModifiers:       requiresReplaceStr,
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "SSH key IDs to inject — reference `gigahost_account_ssh_key` " +
					"resources or the `gigahost_ssh_key` data source.",
				Optional:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.List{requiresReplaceUnlessAdoptingList()},
			},
			"backups": schema.BoolAttribute{
				MarkdownDescription: "Enable backups for the server.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Bool{requiresReplaceUnlessAdoptingBool()},
			},
			"ip": schema.StringAttribute{
				MarkdownDescription: "Primary IPv4 address, available once provisioned.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ipv6": schema.StringAttribute{
				MarkdownDescription: "Primary IPv6 address, if assigned.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Root password returned at deploy time, rotated by an in-place " +
					"reinstall. Shown once per install.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Provisioning/runtime status (running, off, installing, rescue).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cores": schema.Int64Attribute{
				MarkdownDescription: "vCPU cores of the resolved size.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"memory_gb": schema.Int64Attribute{
				MarkdownDescription: "Memory in GB of the resolved size.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"storage_gb": schema.Int64Attribute{
				MarkdownDescription: "Total storage in GB of the resolved size.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"rate_hourly": schema.Float64Attribute{
				MarkdownDescription: "Hourly rate of the resolved size at deploy time.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
			"rate_monthly": schema.Float64Attribute{
				MarkdownDescription: "Monthly cap of the resolved size at deploy time.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
			},
			"primary_ip_id": schema.StringAttribute{
				MarkdownDescription: "ID of the primary IPv4 address. Use this to wire " +
					"`gigahost_server_rdns.ip_id` without a separate data source lookup.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"location": schema.StringAttribute{
				MarkdownDescription: "Datacenter location reported by the API, e.g. `DC2`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vps_type": schema.StringAttribute{
				MarkdownDescription: "Virtualization type, e.g. `kvm`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"suspended": schema.BoolAttribute{
				MarkdownDescription: "Whether the server is suspended.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"bandwidth": schema.Int64Attribute{
				MarkdownDescription: "Bandwidth allocation reported by the API.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "Server creation time (Unix seconds).",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"os_name": schema.StringAttribute{
				MarkdownDescription: "Operating system name reported by the live server.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"os_release": schema.StringAttribute{
				MarkdownDescription: "Operating system release reported by the live server.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ips": schema.ListNestedAttribute{
				MarkdownDescription: "All IP addresses assigned to the server. `id`/`subnet_id` feed " +
					"`gigahost_server_rdns` and `gigahost_bgp_session`.",
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.StringAttribute{Computed: true},
						"subnet_id": schema.StringAttribute{Computed: true},
						"version":   schema.StringAttribute{Computed: true},
						"address":   schema.StringAttribute{Computed: true},
						"reverse":   schema.StringAttribute{Computed: true},
						"type":      schema.StringAttribute{Computed: true},
					},
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
			}),
		},
	}
}

// Configure receives the shared client.
func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create resolves the slugs against the live catalog, deploys a server, and
// waits for it to become ready.
func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	cat, err := r.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	product, err := cat.FindProduct(gigahost.ProductSelector{
		Platform: plan.Platform.ValueString(),
		Type:     plan.Type.ValueString(),
		Size:     plan.Size.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("No matching server size", err.Error())

		return
	}

	if product.PlatformSlug() != gigahost.PlatformCloud {
		resp.Diagnostics.AddError("Unsupported platform",
			fmt.Sprintf("size %s is a %s product; only cloud servers can be deployed through this resource yet",
				product.SizeSlug(), product.PlatformSlug()))

		return
	}

	region, err := cat.RegionForProduct(product, plan.Region.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve region", err.Error())

		return
	}

	deployReq := gigahost.DeployServerRequest{
		ProductID: product.ID,
		PriceID:   product.PriceID,
		RegionID:  region.ID,
		Quantity:  1,
		Backups:   plan.Backups.ValueBool(),
		Rescue:    plan.Rescue.ValueBool(),
	}

	if os := plan.OS.ValueString(); os != "" {
		resolved, rerr := r.client.Reinstall.ResolveOS(ctx, os)
		if rerr != nil {
			resp.Diagnostics.AddError("No matching operating system", rerr.Error())

			return
		}

		deployReq.OSID = resolved.OS.ID
	}

	if iso := plan.ISO.ValueString(); iso != "" {
		resolved, rerr := r.client.Deploy.ResolveISO(ctx, iso)
		if rerr != nil {
			resp.Diagnostics.AddError("No matching ISO", rerr.Error())

			return
		}

		deployReq.ISOID = resolved.ID
	}

	sshKeys, diags := listToStrings(ctx, plan.SSHKeys)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	deployReq.SSHKeys = sshKeys

	if !plan.Hostname.IsNull() && plan.Hostname.ValueString() != "" {
		deployReq.Hostnames = []string{plan.Hostname.ValueString()}
	}

	// Resolved catalog facts, recorded at deploy time. Set before the deploy
	// call so a partial state persisted on failure carries known (not unknown)
	// computed values — the framework rejects unknowns in saved state.
	plan.fillCatalogFacts(product, region)

	timeout, diags := plan.Timeouts.Create(ctx, deployReadyTimeout)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	deployResp, err := r.client.Deploy.Deploy(ctx, deployReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to deploy server", err.Error())

		return
	}

	if len(deployResp.OrderIDs) == 0 {
		resp.Diagnostics.AddError("Deploy returned no orders", "the API accepted the deploy but returned no order IDs")

		return
	}

	// The order is placed (and billed) from here on, so every failure path must
	// persist state: a partial resource is tainted and cancellable, not an
	// orphaned billed server invisible to Terraform.
	orderID := deployResp.OrderIDs[0]
	plan.OrderID = types.StringValue(orderID)

	result, err := r.waitForServer(ctx, orderID, timeout)
	if result != nil && result.serverID != "" {
		plan.ID = types.StringValue(result.serverID)
	}

	if err != nil {
		r.failCreateTainted(ctx, resp, plan, orderID, err)

		return
	}

	plan.ID = types.StringValue(result.serverID)
	plan.IP = stringOrNull(result.ip)
	plan.IPv6 = stringOrNull(result.ipv6)
	plan.Password = stringOrNull(result.password)
	plan.Status = types.StringValue(result.status)

	// Fetch the server record to fill cores + primary IPv4 ID. A failure here
	// means the server deployed but we cannot read its details: keep it in state
	// tainted rather than silently writing catalog-fallback values as if the
	// read succeeded.
	srv, gerr := r.client.Servers.Get(ctx, result.serverID)
	if gerr != nil {
		plan.nullUnknownRuntime()
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		resp.Diagnostics.AddError("Server deployed but reading its details failed",
			fmt.Sprintf("server %s (order %s): %v\n\nThe server was saved to Terraform state and marked "+
				"tainted; the next apply will replace it (cancelling this one).", result.serverID, orderID, gerr))

		return
	}

	plan.Cores = types.Int64Value(int64(srv.Cores))
	plan.PrimaryIPID = types.StringValue(primaryIPv4ID(srv))
	resp.Diagnostics.Append(plan.fillRuntimeFromServer(ctx, srv)...)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// fillCatalogFacts records the deploy-time catalog facts on the model. Cores is
// seeded from the catalog as a fallback; Create overwrites it from the live
// server record on success.
func (m *serverResourceModel) fillCatalogFacts(product *gigahost.DeployProduct, region *gigahost.DeployRegion) {
	m.Platform = types.StringValue(product.PlatformSlug())
	m.Region = types.StringValue(region.Slug())
	m.MemoryGB = types.Int64Value(int64(product.Specs.RAMGB))

	storage := 0
	for _, disk := range product.Specs.Disks {
		storage += disk.SizeGB
	}

	m.StorageGB = types.Int64Value(int64(storage))
	m.RateHourly = types.Float64Value(product.RateHourly)
	m.RateMonthly = types.Float64Value(product.RateMonthly)
	m.Cores = types.Int64Value(int64(product.Specs.CPUCores))
}

// fillRuntimeFromServer mirrors read-only runtime facts from a live server
// record onto the model. It returns diagnostics from building the ips list.
func (m *serverResourceModel) fillRuntimeFromServer(ctx context.Context, srv *gigahost.Server) diag.Diagnostics {
	m.Location = stringOrNull(srv.Location)
	m.VPSType = stringOrNull(srv.VPSType)
	m.Suspended = types.BoolValue(srv.Suspended)
	m.Bandwidth = types.Int64Value(int64(srv.Bandwidth))
	m.CreatedAt = types.Int64Value(srv.CreatedAt.Unix())

	if srv.OS != nil {
		m.OSName = stringOrNull(srv.OS.Name)
		m.OSRelease = stringOrNull(srv.OS.Release)
	} else {
		m.OSName = types.StringNull()
		m.OSRelease = types.StringNull()
	}

	ips := make([]serverIPModel, 0, len(srv.IPs))
	for i := range srv.IPs {
		ip := srv.IPs[i]
		ips = append(ips, serverIPModel{
			ID:       types.StringValue(ip.ID),
			SubnetID: types.StringValue(ip.SubnetID),
			Version:  types.StringValue(ip.Version),
			Address:  types.StringValue(ip.Address),
			Reverse:  types.StringValue(ip.Reverse),
			Type:     types.StringValue(ip.Type),
		})
	}

	list, diags := types.ListValueFrom(ctx, serverIPObjectType(), ips)
	m.IPs = list

	return diags
}

// nullUnknownRuntime sets every Computed runtime attribute that may still be
// unknown to a concrete value, so the model can be written to state on a
// failure path (saved state may not contain unknowns).
func (m *serverResourceModel) nullUnknownRuntime() {
	for _, p := range []*types.String{
		&m.ID, &m.IP, &m.IPv6, &m.Password, &m.Status, &m.PrimaryIPID,
		&m.Location, &m.VPSType, &m.OSName, &m.OSRelease,
	} {
		if p.IsUnknown() {
			*p = types.StringNull()
		}
	}

	if m.Cores.IsUnknown() {
		m.Cores = types.Int64Null()
	}

	if m.Bandwidth.IsUnknown() {
		m.Bandwidth = types.Int64Null()
	}

	if m.CreatedAt.IsUnknown() {
		m.CreatedAt = types.Int64Null()
	}

	if m.Suspended.IsUnknown() {
		m.Suspended = types.BoolNull()
	}

	if m.IPs.IsUnknown() {
		m.IPs = types.ListNull(serverIPObjectType())
	}
}

// failCreateTainted persists a partial server to state and reports the deploy
// failure as an error, so the framework marks the resource tainted and a
// subsequent destroy/apply cancels the billed server.
func (r *serverResource) failCreateTainted(ctx context.Context, resp *resource.CreateResponse, plan serverResourceModel, orderID string, cause error) {
	plan.nullUnknownRuntime()
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)

	var hint string
	if plan.ID.IsNull() || plan.ID.ValueString() == "" {
		hint = fmt.Sprintf("No server id was observed for order %s. It was saved to state and marked "+
			"tainted; the next apply will retry. If billing started, cancel order %s in the Gigahost "+
			"control panel.", orderID, orderID)
	} else {
		hint = fmt.Sprintf("Server %s (order %s) was saved to state and marked tainted; the next apply "+
			"will replace it, cancelling this one.", plan.ID.ValueString(), orderID)
	}

	resp.Diagnostics.AddError("Server did not become ready", fmt.Sprintf("%v\n\n%s", cause, hint))
}

// primaryIPv4ID returns the ID of the server's primary IPv4 address. It matches
// by address (the version field encoding is unreliable): first the IP equal to
// the server's primary IP, then any dotted (IPv4) address.
func primaryIPv4ID(srv *gigahost.Server) string {
	for _, ip := range srv.IPs {
		if ip.ID != "" && ip.Address == srv.PrimaryIP {
			return ip.ID
		}
	}

	for _, ip := range srv.IPs {
		if ip.ID != "" && strings.Contains(ip.Address, ".") {
			return ip.ID
		}
	}

	return ""
}

// deployResult is the unified completion record returned by waitForServer.
// Fields are filled from whichever source observed them last — the status view
// while the order lives there, then the durable server record. serverID is the
// most important: once set, a failed wait still leaves enough in state to
// cancel the billed server.
type deployResult struct {
	serverID string
	ip       string
	ipv6     string
	password string
	status   string
}

// knownProvisioningStatuses are the non-terminal /deploy/status values. Any
// status outside this set that is not a known ready state is treated as a
// failure — the API has no explicit failure status, so this is best-effort
// defensive detection rather than a documented contract.
var knownProvisioningStatuses = map[gigahost.DeployProvisionStatus]bool{
	gigahost.DeployStatusWaiting:    true,
	gigahost.DeployStatusDeploying:  true,
	gigahost.DeployStatusInstalling: true,
}

// terminalReadyStatuses are the /deploy/status values that mean the server is
// usable (provisioning finished).
var terminalReadyStatuses = map[gigahost.DeployProvisionStatus]bool{
	gigahost.DeployStatusReady:  true,
	gigahost.DeployStatusRescue: true,
	gigahost.DeployStatusISO:    true,
}

// statusForOrder returns the status entry matching orderID, or nil if the
// order is not present in this status snapshot.
func statusForOrder(st *gigahost.DeployStatus, orderID string) *gigahost.DeployServerStatus {
	if st == nil {
		return nil
	}

	for i := range st.Servers {
		if st.Servers[i].OrderID == orderID {
			return &st.Servers[i]
		}
	}

	return nil
}

// statusIsFailure reports whether a /deploy/status status string signals a
// failed deploy. The known provisioning and ready states are never failures;
// anything else (empty excluded) is treated defensively as a failure.
func statusIsFailure(s gigahost.DeployProvisionStatus) bool {
	if s == "" || knownProvisioningStatuses[s] || terminalReadyStatuses[s] {
		return false
	}

	return true
}

// resultFromStatus projects a status entry into a deployResult.
func resultFromStatus(e gigahost.DeployServerStatus) deployResult {
	return deployResult{
		serverID: e.ServerID,
		ip:       e.IP,
		ipv6:     e.IPv6,
		password: e.Password,
		status:   string(e.Status),
	}
}

// serverIsReady reports whether a durable server record shows provisioning is
// complete (installed and powered/rescue, not mid-install).
func serverIsReady(srv *gigahost.Server) bool {
	return srv != nil && !srv.StatusInstall && (srv.Status || srv.StatusRescue)
}

// waitForServer polls /deploy/status until the server for orderID is ready,
// falling back to the durable /servers record once a server id has appeared but
// the order has dropped from the status view. It returns the last observed
// result even on error, so a failed deploy can still be cancelled by its id.
func (r *serverResource) waitForServer(ctx context.Context, orderID string, timeout time.Duration) (*deployResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(r.poll())
	defer ticker.Stop()

	var last *deployResult

	statusErrs := 0
	statusMisses := 0
	goneChecks := 0

	for {
		st, err := r.client.Deploy.GetStatus(ctx, []string{orderID})
		switch {
		case err != nil:
			statusErrs++
			if statusErrs > maxStatusPollErrors {
				return last, fmt.Errorf("deploy status failed %d times in a row: %w", statusErrs, err)
			}
		default:
			statusErrs = 0

			if entry := statusForOrder(st, orderID); entry != nil {
				statusMisses = 0
				goneChecks = 0
				res := resultFromStatus(*entry)
				last = &res

				if st.AllReady || terminalReadyStatuses[entry.Status] {
					return last, nil
				}

				if statusIsFailure(entry.Status) {
					return last, fmt.Errorf("server (order %s) reported failure status %q", orderID, entry.Status)
				}
			} else {
				// The order has dropped from the status view. Once a server id
				// is known, the server record is the durable completion source.
				statusMisses++

				if last != nil && last.serverID != "" && statusMisses%listFallbackEvery == 0 {
					srv, gerr := r.client.Servers.Get(ctx, last.serverID)
					switch {
					case gerr == nil:
						goneChecks = 0

						if serverIsReady(srv) {
							mergeServerIntoResult(last, srv)

							return last, nil
						}
					case gigahost.IsNotFound(gerr):
						goneChecks++
						if goneChecks >= maxGoneChecks {
							return last, fmt.Errorf("server (order %s, id %s) disappeared while provisioning", orderID, last.serverID)
						}
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}

// mergeServerIntoResult fills runtime fields from the durable server record
// when the status view did not report them.
func mergeServerIntoResult(res *deployResult, srv *gigahost.Server) {
	if srv == nil {
		return
	}

	if srv.PrimaryIP != "" {
		res.ip = srv.PrimaryIP
	}

	for _, ip := range srv.IPs {
		if strings.Contains(ip.Address, ":") {
			res.ipv6 = ip.Address

			break
		}
	}

	res.status = serverStatusString(srv)
}

// ModifyPlan decides how an os change is applied: between two OS slugs it
// is an in-place reinstall (ID + IP kept, disk wiped) with a loud warning;
// any transition involving iso/rescue (or clearing os) replaces the server.
func (r *serverResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Create or destroy: nothing to decide.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state, plan serverResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Adoption: a freshly imported server (product selectors absent from prior
	// state) is being reconciled, not mutated. The first apply runs the
	// adoption branch in Update, which never reinstalls — so do not classify an
	// os difference here as a reinstall (which would warn and blank the
	// password). Read already aligned os with the live OSID; any residual
	// textual difference is slug aliasing, not a real change.
	if state.Type.IsNull() && state.Size.IsNull() {
		return
	}

	if state.OS.Equal(plan.OS) {
		return
	}

	// Transitions to or from iso/rescue (or clearing os) replace.
	if state.OS.IsNull() || plan.OS.IsNull() || plan.OS.IsUnknown() {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("os"))

		return
	}

	// In-place reinstall: the rotated computed fields become unknown.
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("password"), types.StringUnknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("status"), types.StringUnknown())...)

	resp.Diagnostics.AddWarning(
		"In-place OS reinstall",
		fmt.Sprintf("Changing os from %s to %s reinstalls the operating system on server %s in place: "+
			"the server ID and IP are kept, but ALL DATA ON DISK IS WIPED and SSH keys are not re-injected. "+
			"The root password rotates (see the password attribute after apply).",
			state.OS.ValueString(), plan.OS.ValueString(), state.ID.ValueString()),
	)
}

// Read refreshes runtime attributes and the few deploy selectors the API can
// be coaxed into round-tripping, so a freshly imported server converges.
//
// The product-derived selectors (type/size/platform) cannot be recovered —
// live servers report product_id "0" — and are adopted from config on the
// first apply (see Update). What Read can populate it does: hostname, os
// (OSID resolved to a catalog slug), and region (Location matched against the
// catalog best-effort). For an already-managed server these are written back
// idempotently: an os slug that still resolves to the live OSID is left in
// the user's chosen form rather than rewritten to the canonical slug.
//
// A definitive not-found (HTTP 404) removes the resource; any other error is
// surfaced so a transient failure does not masquerade as a cancelled server.
func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// A partially-created server (deploy wait failed before an id was seen) has
	// only an order_id in state. Adopt it by the order once it appears; never
	// treat it as deleted while the order is unresolved.
	if state.ID.IsNull() || state.ID.ValueString() == "" {
		id := r.discoverServerByOrder(ctx, state.OrderID)
		if id == "" {
			resp.Diagnostics.AddWarning("Gigahost server not yet identified",
				fmt.Sprintf("order %s has not yet produced a server id; keeping the resource in state. "+
					"Re-run apply once provisioning progresses.", state.OrderID.ValueString()))
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

			return
		}

		state.ID = types.StringValue(id)
	}

	srv, err := r.findServerConfirmed(ctx, state.ID.ValueString())
	if err != nil {
		// Absence is only definitive after repeated confirmation (findServerConfirmed
		// re-reads). Any other error (transient/5xx/auth) must surface, or
		// `terraform import` on a momentarily-failing API reports "Cannot import
		// non-existent remote object".
		if gigahost.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Failed to read server",
			fmt.Sprintf("server %s: %v", state.ID.ValueString(), err))

		return
	}

	if srv.PrimaryIP != "" {
		state.IP = types.StringValue(srv.PrimaryIP)
	}

	// The server list does not always expose the IPv6 address that deploy
	// reported, so a known address is kept rather than nulled; an empty/unknown
	// prior collapses to null instead of "".
	for _, ip := range srv.IPs {
		if strings.Contains(ip.Address, ":") {
			state.IPv6 = types.StringValue(ip.Address)

			break
		}
	}

	if state.IPv6.IsUnknown() || state.IPv6.ValueString() == "" {
		state.IPv6 = types.StringNull()
	}

	state.Status = types.StringValue(serverStatusString(srv))
	state.Cores = types.Int64Value(int64(srv.Cores))
	state.PrimaryIPID = types.StringValue(primaryIPv4ID(srv))
	resp.Diagnostics.Append(state.fillRuntimeFromServer(ctx, srv)...)

	if srv.Hostname != "" {
		state.Hostname = types.StringValue(srv.Hostname)
	}

	state.OS = r.refreshOS(ctx, state.OS, srv.OSID)
	state.Region = r.refreshRegion(ctx, state.Region, srv.Location)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// findServerConfirmed reads a server by id, re-reading on a not-found before
// concluding absence: the /servers view transiently omits a live server for
// tens of seconds (observed live), so a single miss must not drop a billed
// server from state. A non-404 error is returned immediately (it is transient,
// not a deletion). The returned error satisfies IsNotFound only after the
// absence is confirmed across serverAbsenceReads reads.
func (r *serverResource) findServerConfirmed(ctx context.Context, id string) (*gigahost.Server, error) {
	var lastErr error

	for attempt := 1; attempt <= serverAbsenceReads; attempt++ {
		srv, err := r.client.Servers.Get(ctx, id)
		if err == nil {
			return srv, nil
		}

		if !gigahost.IsNotFound(err) {
			return nil, err
		}

		lastErr = err

		if attempt < serverAbsenceReads {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(r.absence()):
			}
		}
	}

	return nil, lastErr
}

// discoverServerByOrder asks /deploy/status for the server id of an in-flight
// order, the only bridge from order to server (the /servers view carries no
// order linkage). Returns "" if the order is unknown, already dropped from the
// status view, or has not yet produced a server.
func (r *serverResource) discoverServerByOrder(ctx context.Context, order types.String) string {
	if order.IsNull() || order.ValueString() == "" {
		return ""
	}

	st, err := r.client.Deploy.GetStatus(ctx, []string{order.ValueString()})
	if err != nil {
		return ""
	}

	if e := statusForOrder(st, order.ValueString()); e != nil {
		return e.ServerID
	}

	return ""
}

// refreshOS resolves the live server's OSID to a catalog OS slug. To avoid
// churning a user's chosen form (slug aliasing — "bookworm" and "debian-12"
// resolve to the same OSID), a non-null prior value that still resolves to the
// live OSID is kept as-is. A null prior (just imported) or one that no longer
// matches the live OSID is repopulated from the catalog. Resolution failures
// or unknown OSIDs leave the prior value untouched — Read never invents drift.
func (r *serverResource) refreshOS(ctx context.Context, prior types.String, osID string) types.String {
	if osID == "" {
		return prior
	}

	if !prior.IsNull() {
		if resolved, err := r.client.Reinstall.ResolveOS(ctx, prior.ValueString()); err == nil && resolved.OS.ID == osID {
			return prior
		}
	}

	all, err := r.client.Reinstall.ListAllOperatingSystems(ctx)
	if err != nil {
		return prior
	}

	for _, o := range all {
		if o.OS.ID == osID {
			return types.StringValue(o.Slug)
		}
	}

	return prior
}

// osDisplay best-effort renders a live OSID as a human-readable OS name for
// diagnostics. It falls back to the raw OSID if the catalog cannot be read or
// the ID is unknown, so an error message never goes empty.
func (r *serverResource) osDisplay(ctx context.Context, osID string) string {
	all, err := r.client.Reinstall.ListAllOperatingSystems(ctx)
	if err != nil {
		return osID
	}

	for _, o := range all {
		if o.OS.ID == osID {
			return o.OS.Name
		}
	}

	return osID
}

// refreshRegion best-effort maps the live server's Location to a catalog
// region slug. Location lives in a different namespace than catalog region IDs,
// so it is matched case-insensitively against each region's slug and name; an
// unmatched Location (or catalog read failure) preserves the prior value,
// including null.
func (r *serverResource) refreshRegion(ctx context.Context, prior types.String, location string) types.String {
	if location == "" {
		return prior
	}

	cat, err := r.client.Deploy.GetCatalog(ctx)
	if err != nil {
		return prior
	}

	for i := range cat.Regions {
		region := cat.Regions[i]
		if strings.EqualFold(location, region.Slug()) || strings.EqualFold(location, region.Name) {
			return types.StringValue(region.Slug())
		}
	}

	return prior
}

// serverStatusString derives a coarse status string from a server record.
func serverStatusString(srv *gigahost.Server) string {
	switch {
	case srv.StatusInstall:
		return "installing"
	case srv.StatusRescue:
		return "rescue"
	case srv.Status:
		return "running"
	default:
		return "off"
	}
}

// Update either adopts a freshly imported server's config into state or
// applies an in-place OS reinstall when os changed. ModifyPlan and the
// adoption-aware plan modifiers route every other change to replacement.
//
// Adoption fires when the product selectors are null in prior state — the
// signature of `terraform import`, where Read could not recover them. It
// verifies the config against the live machine and writes the config values
// into state rather than mutating the server. A normal update (selectors
// present in state) takes the os-reinstall path below.
func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan serverResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Adoption: prior state never carried the product selectors (just
	// imported). Verify config against the live machine, then accept it —
	// never reinstall, the server already runs what the user is adopting.
	if state.Type.IsNull() && state.Size.IsNull() {
		r.adoptImported(ctx, state, plan, resp)

		return
	}

	if !state.OS.Equal(plan.OS) {
		resolved, err := r.client.Reinstall.ResolveOS(ctx, plan.OS.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("No matching operating system", err.Error())

			return
		}

		res, err := r.client.Reinstall.Reinstall(ctx, state.ID.ValueString(), gigahost.ReinstallRequest{
			OSID:     resolved.OS.ID,
			Hostname: plan.Hostname.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to reinstall server", err.Error())

			return
		}

		if err := r.waitForInstall(ctx, state.ID.ValueString()); err != nil {
			resp.Diagnostics.AddError("Reinstall did not settle",
				fmt.Sprintf("server %s: %v", state.ID.ValueString(), err))

			return
		}

		plan.Password = types.StringValue(res.RootPasswd)
	}

	if srv, gerr := r.client.Servers.Get(ctx, state.ID.ValueString()); gerr == nil {
		plan.Status = types.StringValue(serverStatusString(srv))
		resp.Diagnostics.Append(plan.fillRuntimeFromServer(ctx, srv)...)
	} else {
		plan.Status = types.StringValue("unknown")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// adoptImported converges a freshly imported server: it resolves the config's
// type/size against the live catalog exactly like Create, confirms the
// resolved product's hardware matches the running machine, and — on a match —
// writes the config values into state. Nothing on the server changes; this
// only teaches Terraform the selectors the API cannot round-trip.
//
// type/size/platform/region are verifiable (catalog + live specs) and are
// adopted only after the cores/RAM check passes. A non-null config os is
// resolved and verified against the live OSID — adopting must never write a
// slug to state that the running machine does not match, which would silently
// diverge config from reality (Read populated the live slug at import, but the
// first apply would overwrite it without a reinstall). A null config os keeps
// the live slug Read already set. hostname was reconciled by Read against the
// live machine. ssh_keys/backups/iso/rescue are deploy-time-only inputs the
// live API does not report, so they are adopted as configured and trusted —
// there is nothing to verify them against.
func (r *serverResource) adoptImported(ctx context.Context, state, plan serverResourceModel, resp *resource.UpdateResponse) {
	srv, err := r.client.Servers.Get(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read server for adoption",
			fmt.Sprintf("server %s: %v", state.ID.ValueString(), err))

		return
	}

	cat, err := r.client.Deploy.GetCatalog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read deploy catalog", err.Error())

		return
	}

	product, err := cat.FindProduct(gigahost.ProductSelector{
		Platform: plan.Platform.ValueString(),
		Type:     plan.Type.ValueString(),
		Size:     plan.Size.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("No matching server size", err.Error())

		return
	}

	// The live /servers srv_ram is in GB (verified 2026-06: API docs example
	// "2", catalog product names like "4GB", and the catalog's own ram_gb are
	// all GB). ramGB normalizes defensively in case a product type ever reports
	// MB instead, so adoption does not falsely reject a matching machine.
	if product.Specs.CPUCores != srv.Cores || product.Specs.RAMGB != ramGB(srv.RAM) {
		resp.Diagnostics.AddError(
			"Imported server does not match configured size",
			fmt.Sprintf("imported server %s has %d cores / %d GB RAM; config type %q size %q "+
				"resolves to %d cores / %d GB RAM. Adjust type/size to match the live machine "+
				"(it cannot be resized in place — a change replaces the server).",
				state.ID.ValueString(), srv.Cores, ramGB(srv.RAM),
				plan.Type.ValueString(), plan.Size.ValueString(),
				product.Specs.CPUCores, product.Specs.RAMGB),
		)

		return
	}

	// A non-null config os must match what the machine actually runs. Adoption
	// never reinstalls, so a mismatch would write the wrong slug to state (Read
	// set the live slug at import; this apply overwrites it) and the divergence
	// would persist silently. Resolve the same way Create does and compare the
	// resolved OS ID against the live OSID. A null config os is left for Read's
	// live slug; no verification needed.
	if !plan.OS.IsNull() {
		resolved, err := r.client.Reinstall.ResolveOS(ctx, plan.OS.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to resolve configured operating system", err.Error())

			return
		}

		if resolved.OS.ID != srv.OSID {
			resp.Diagnostics.AddError(
				"Imported server does not match configured os",
				fmt.Sprintf("imported server %s runs %q (os_id %s); config os %q resolves to %q "+
					"(os_id %s). Set os to the live operating system to adopt, then run a normal "+
					"os change AFTER adopting if you want to reinstall.",
					state.ID.ValueString(), r.osDisplay(ctx, srv.OSID), srv.OSID,
					plan.OS.ValueString(), resolved.OS.Name, resolved.OS.ID),
			)

			return
		}
	}

	// Verified facts: adopt the config selectors and record the platform the
	// catalog actually resolved (config may have relied on its default).
	plan.Platform = types.StringValue(product.PlatformSlug())

	// region: prefer config; otherwise keep what Read mapped from Location.
	if plan.Region.IsNull() || plan.Region.IsUnknown() {
		plan.Region = state.Region
	}

	plan.Status = types.StringValue(serverStatusString(srv))
	resp.Diagnostics.Append(plan.fillRuntimeFromServer(ctx, srv)...)

	// Computed deploy facts. These are Computed+UseStateForUnknown, but import
	// leaves them null in prior state, so the framework cannot copy a value into
	// the plan — it stays unknown. Fill them from the resolved catalog product
	// exactly as Create does, or the first apply after import fails with
	// "provider produced inconsistent result" (plan unknown, state null).
	plan.MemoryGB = types.Int64Value(int64(product.Specs.RAMGB))

	storage := 0
	for _, disk := range product.Specs.Disks {
		storage += disk.SizeGB
	}

	plan.StorageGB = types.Int64Value(int64(storage))
	plan.RateHourly = types.Float64Value(product.RateHourly)
	plan.RateMonthly = types.Float64Value(product.RateMonthly)

	// The deploy-time root password is not recoverable for an imported server.
	// Pin it to null deterministically rather than leaving it unknown, which
	// can surface as an inconsistent-result warning on the adopting apply.
	plan.Password = types.StringNull()

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// ramGB normalizes a server's reported RAM to GB. The Gigahost API reports
// srv_ram in GB (verified against the API docs and catalog), but a value that
// is implausibly large for GB and an exact multiple of 1024 is treated as a
// mistaken MB report and converted, so size adoption never falsely rejects a
// matching machine.
func ramGB(v int) int {
	if v >= 1024 && v%1024 == 0 {
		return v / 1024
	}

	return v
}

// waitForInstall polls until the server reports it is no longer installing.
func (r *serverResource) waitForInstall(ctx context.Context, serverID string) error {
	ctx, cancel := context.WithTimeout(ctx, reinstallTimeout)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		srv, err := r.client.Servers.Get(ctx, serverID)
		if err == nil && !srv.StatusInstall {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Delete cancels the server, stopping hourly billing. A server that died during
// provisioning cannot be cancelled (the API refuses with a non-404 error rather
// than 404), so a refused cancellation is followed by an absence check: a
// confirmed-gone server is cleared from state with a warning instead of failing
// destroy forever.
func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// A partially-created server never got an id (the deploy wait failed first).
	// It cannot be cancelled by id; direct the user to the order.
	if state.ID.IsNull() || state.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Unable to cancel server",
			fmt.Sprintf("the server has no id in state. If order %s started billing, cancel it in the "+
				"Gigahost control panel.", state.OrderID.ValueString()))

		return
	}

	err := r.client.Servers.Cancel(ctx, state.ID.ValueString())
	if err == nil {
		return
	}

	// Cancelling a nonexistent server is refused with a non-404 error, so a
	// refusal is reconciled against the server list before it counts as fatal.
	if !gigahost.IsNotFound(err) {
		if _, findErr := r.findServerConfirmed(ctx, state.ID.ValueString()); findErr != nil && gigahost.IsNotFound(findErr) {
			resp.Diagnostics.AddWarning("Gigahost server already gone",
				fmt.Sprintf("server %s no longer exists, so the cancellation was refused (%v). Clearing it "+
					"from state.", state.ID.ValueString(), err))

			return
		}
	}

	// A definitive 404 means the server is already cancelled — treat as success.
	if gigahost.IsNotFound(err) {
		return
	}

	resp.Diagnostics.AddError("Failed to cancel server", err.Error())
}

// ImportState imports by server ID and adopts the configuration rather than
// replacing the server. The product selectors (type/size/platform) cannot be
// round-tripped from the live API, so import stores only the ID; Read then
// repopulates what it can (hostname, os, region best-effort, runtime facts)
// and the adoption-aware plan modifiers keep the null selectors from forcing a
// replace. The first apply runs the adoption branch in Update, which verifies
// the configured type/size against the live machine's cores/RAM and writes the
// config values into state. Supply a config matching the live server: a
// genuine mismatch fails loudly instead of silently destroying it.
func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
