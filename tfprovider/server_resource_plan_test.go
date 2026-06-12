package tfprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestServerModifyPlanMatrix verifies how every os/iso/rescue transition is
// classified, including the iso-sourced transitions that are unreachable
// live (the test account has no uploaded ISOs). The live counterpart is
// TestAccServerOSChangeMatrix.
func TestServerModifyPlanMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		state, plan    *bootSource // nil = resource being created/destroyed
		wantReplace    bool
		wantWarning    bool
		wantPwdUnknown bool
	}{
		{name: "create", state: nil, plan: &bootSource{os: "debian-11"}},
		{name: "destroy", state: &bootSource{os: "debian-11"}, plan: nil},
		{name: "same os is noop", state: &bootSource{os: "debian-11"}, plan: &bootSource{os: "debian-11"}},
		{
			name:  "os to os reinstalls in place",
			state: &bootSource{os: "debian-11"}, plan: &bootSource{os: "debian-12"},
			wantWarning: true, wantPwdUnknown: true,
		},
		{
			name:  "os to iso replaces",
			state: &bootSource{os: "debian-11"}, plan: &bootSource{iso: "custom.iso"},
			wantReplace: true,
		},
		{
			name:  "os to rescue replaces",
			state: &bootSource{os: "debian-11"}, plan: &bootSource{rescue: true},
			wantReplace: true,
		},
		{
			name:  "iso to os replaces",
			state: &bootSource{iso: "custom.iso"}, plan: &bootSource{os: "debian-11"},
			wantReplace: true,
		},
		{
			name:  "rescue to os replaces",
			state: &bootSource{rescue: true}, plan: &bootSource{os: "debian-11"},
			wantReplace: true,
		},
		{
			// Just-imported (null type/size in state): an os difference must
			// not be classified as a reinstall — the first apply adopts config,
			// it does not mutate the machine. No warning, no blanked password.
			name:  "adopting suppresses os reinstall",
			state: &bootSource{os: "debian-11", adopting: true}, plan: &bootSource{os: "debian-12"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := &serverResource{}

			req := resource.ModifyPlanRequest{
				State: tfsdk.State{Schema: serverTestSchema(t), Raw: serverRawValue(tc.state)},
				Plan:  tfsdk.Plan{Schema: serverTestSchema(t), Raw: serverRawValue(tc.plan)},
			}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}

			r.ModifyPlan(context.Background(), req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected error diagnostics: %v", resp.Diagnostics)
			}

			if gotReplace := len(resp.RequiresReplace) > 0; gotReplace != tc.wantReplace {
				t.Errorf("RequiresReplace = %v, want %v", resp.RequiresReplace, tc.wantReplace)
			}

			if gotWarning := resp.Diagnostics.WarningsCount() > 0; gotWarning != tc.wantWarning {
				t.Errorf("warnings = %d, want warning=%v", resp.Diagnostics.WarningsCount(), tc.wantWarning)
			}

			if tc.wantPwdUnknown {
				var m map[string]tftypes.Value
				if err := resp.Plan.Raw.As(&m); err != nil {
					t.Fatalf("decode plan: %v", err)
				}

				if m["password"].IsKnown() {
					t.Errorf("password should be unknown after reinstall plan, got %v", m["password"])
				}
			}
		})
	}
}

// TestRequiresReplaceUnlessAdopting verifies the adoption-aware replace
// modifiers at the modifier level: a known, differing prior state value
// forces replacement; a null prior value (the just-imported case) adopts the
// config value in place; an equal value is a no-op. The non-null State.Raw /
// Plan.Raw guard the framework's create/destroy short-circuits so only the
// adoption logic is exercised.
func TestRequiresReplaceUnlessAdopting(t *testing.T) {
	t.Parallel()

	// Non-null raw objects so RequiresReplaceIf does not treat the case as a
	// create (null State.Raw) or destroy (null Plan.Raw).
	rawState := tfsdk.State{Schema: serverTestSchema(t), Raw: serverRawValue(&bootSource{os: "debian-11"})}
	rawPlan := tfsdk.Plan{Schema: serverTestSchema(t), Raw: serverRawValue(&bootSource{os: "debian-12"})}

	cases := []struct {
		name        string
		state, plan types.String
		wantReplace bool
	}{
		{name: "adopt null state in place", state: types.StringNull(), plan: types.StringValue("b"), wantReplace: false},
		{name: "changed value replaces", state: types.StringValue("a"), plan: types.StringValue("b"), wantReplace: true},
		{name: "equal value is noop", state: types.StringValue("a"), plan: types.StringValue("a"), wantReplace: false},
		// region is Optional+Computed with UseStateForUnknown: omitting it from
		// config yields an unknown planned value that the next modifier fills
		// from prior state. An unknown plan must never force a replacement.
		{name: "unknown plan over known state is noop", state: types.StringValue("sfj"), plan: types.StringUnknown(), wantReplace: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// String variant.
			strReq := planmodifier.StringRequest{
				Path:       path.Root("type"),
				State:      rawState,
				Plan:       rawPlan,
				StateValue: tc.state,
				PlanValue:  tc.plan,
			}

			var strResp planmodifier.StringResponse

			requiresReplaceUnlessAdoptingStr().PlanModifyString(context.Background(), strReq, &strResp)

			if strResp.RequiresReplace != tc.wantReplace {
				t.Errorf("string RequiresReplace = %v, want %v", strResp.RequiresReplace, tc.wantReplace)
			}

			// List variant: mirror the string case as a single-element list
			// (null/[a]/[b]).
			listReq := planmodifier.ListRequest{
				Path:       path.Root("ssh_keys"),
				State:      rawState,
				Plan:       rawPlan,
				StateValue: strToList(t, tc.state),
				PlanValue:  strToList(t, tc.plan),
			}

			var listResp planmodifier.ListResponse

			requiresReplaceUnlessAdoptingList().PlanModifyList(context.Background(), listReq, &listResp)

			if listResp.RequiresReplace != tc.wantReplace {
				t.Errorf("list RequiresReplace = %v, want %v", listResp.RequiresReplace, tc.wantReplace)
			}
		})
	}

	// Bool variant: null prior adopts, true<->false replaces, equal no-op.
	boolCases := []struct {
		name        string
		state, plan types.Bool
		wantReplace bool
	}{
		{name: "adopt null bool in place", state: types.BoolNull(), plan: types.BoolValue(true), wantReplace: false},
		{name: "changed bool replaces", state: types.BoolValue(false), plan: types.BoolValue(true), wantReplace: true},
		{name: "equal bool is noop", state: types.BoolValue(true), plan: types.BoolValue(true), wantReplace: false},
		{name: "unknown plan over known bool is noop", state: types.BoolValue(true), plan: types.BoolUnknown(), wantReplace: false},
	}

	for _, tc := range boolCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := planmodifier.BoolRequest{
				Path:       path.Root("backups"),
				State:      rawState,
				Plan:       rawPlan,
				StateValue: tc.state,
				PlanValue:  tc.plan,
			}

			var resp planmodifier.BoolResponse

			requiresReplaceUnlessAdoptingBool().PlanModifyBool(context.Background(), req, &resp)

			if resp.RequiresReplace != tc.wantReplace {
				t.Errorf("bool RequiresReplace = %v, want %v", resp.RequiresReplace, tc.wantReplace)
			}
		})
	}
}

// strToList lifts a types.String into a single-element types.List for
// exercising the list plan modifier with the same matrix: null stays null and
// unknown stays unknown (so the unknown-plan guard is tested), otherwise the
// value becomes a one-element list.
func strToList(t *testing.T, s types.String) types.List {
	t.Helper()

	switch {
	case s.IsNull():
		return types.ListNull(types.StringType)
	case s.IsUnknown():
		return types.ListUnknown(types.StringType)
	}

	l, diags := types.ListValue(types.StringType, []attr.Value{s})
	if diags.HasError() {
		t.Fatalf("strToList: %v", diags)
	}

	return l
}

// TestServerSlugValidator verifies the plan-time syntax gate: obviously
// non-slug values fail validate; well-formed slugs pass. Membership is a
// live-catalog concern checked at apply, never here.
func TestServerSlugValidator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value   string
		wantErr bool
	}{
		{"value", false},
		{"2c-4gb-40gb", false},
		{"debian-12", false},
		{"core-i5-2400-16gb-500gb", false},
		{"KVM Value VPS 4GB", true}, // spaces + uppercase
		{"Value", true},             // uppercase
		{"2c 4gb", true},            // space
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()

			req := validator.StringRequest{
				Path:        path.Root("size"),
				ConfigValue: types.StringValue(tc.value),
			}

			var resp validator.StringResponse

			slugValidator{attr: "size"}.ValidateString(context.Background(), req, &resp)

			if gotErr := resp.Diagnostics.HasError(); gotErr != tc.wantErr {
				t.Errorf("ValidateString(%q) error = %v, want %v (%v)", tc.value, gotErr, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

// bootSource describes which boot input is set. adopting nulls the product
// selectors (type/size), the signature of a freshly imported server whose
// config is being adopted rather than changed.
type bootSource struct {
	os, iso  string
	rescue   bool
	adopting bool
}

// serverTestSchema returns the gigahost_server schema.
func serverTestSchema(t *testing.T) rschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse

	(&serverResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}

	return resp.Schema
}

// serverRawValue builds a raw state/plan value with the given boot source.
// nil yields a null object (resource absent).
func serverRawValue(src *bootSource) tftypes.Value {
	str := tftypes.String
	timeoutsType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"create": str}}
	ipObjType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id": str, "subnet_id": str, "version": str, "address": str, "reverse": str, "type": str,
	}}
	ipsType := tftypes.List{ElementType: ipObjType}
	objType := tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"id": str, "order_id": str, "platform": str, "type": str, "size": str, "region": str,
			"os": str, "iso": str, "rescue": tftypes.Bool,
			"hostname": str, "ssh_keys": tftypes.List{ElementType: str},
			"backups": tftypes.Bool, "ip": str, "ipv6": str, "password": str,
			"status": str, "cores": tftypes.Number, "memory_gb": tftypes.Number,
			"storage_gb": tftypes.Number, "rate_hourly": tftypes.Number,
			"rate_monthly": tftypes.Number, "primary_ip_id": str,
			"location": str, "vps_type": str, "suspended": tftypes.Bool,
			"bandwidth": tftypes.Number, "created_at": tftypes.Number,
			"os_name": str, "os_release": str,
			"ips":      ipsType,
			"timeouts": timeoutsType,
		},
	}

	if src == nil {
		return tftypes.NewValue(objType, nil)
	}

	nullStr := tftypes.NewValue(str, nil)

	strOrNull := func(v string) tftypes.Value {
		if v == "" {
			return nullStr
		}

		return tftypes.NewValue(str, v)
	}

	rescue := tftypes.NewValue(tftypes.Bool, nil)
	if src.rescue {
		rescue = tftypes.NewValue(tftypes.Bool, true)
	}

	typeVal, sizeVal := tftypes.NewValue(str, "value"), tftypes.NewValue(str, "2c-4gb-40gb")
	if src.adopting {
		// Just-imported: Read could not recover the product selectors.
		typeVal, sizeVal = nullStr, nullStr
	}

	return tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(str, "17000"),
		"order_id":      nullStr,
		"platform":      tftypes.NewValue(str, "cloud"),
		"type":          typeVal,
		"size":          sizeVal,
		"region":        tftypes.NewValue(str, "sfj"),
		"os":            strOrNull(src.os),
		"iso":           strOrNull(src.iso),
		"rescue":        rescue,
		"hostname":      nullStr,
		"ssh_keys":      tftypes.NewValue(tftypes.List{ElementType: str}, nil),
		"backups":       tftypes.NewValue(tftypes.Bool, nil),
		"ip":            tftypes.NewValue(str, "185.125.168.10"),
		"ipv6":          nullStr,
		"password":      tftypes.NewValue(str, "secret"),
		"status":        tftypes.NewValue(str, "running"),
		"cores":         tftypes.NewValue(tftypes.Number, 2),
		"memory_gb":     tftypes.NewValue(tftypes.Number, 4),
		"storage_gb":    tftypes.NewValue(tftypes.Number, 40),
		"rate_hourly":   tftypes.NewValue(tftypes.Number, 0.07826),
		"rate_monthly":  tftypes.NewValue(tftypes.Number, 49),
		"primary_ip_id": tftypes.NewValue(str, "4500"),
		"location":      tftypes.NewValue(str, "DC2"),
		"vps_type":      tftypes.NewValue(str, "kvm"),
		"suspended":     tftypes.NewValue(tftypes.Bool, false),
		"bandwidth":     tftypes.NewValue(tftypes.Number, 1000),
		"created_at":    tftypes.NewValue(tftypes.Number, 1530609706),
		"os_name":       strOrNull(""),
		"os_release":    strOrNull(""),
		"ips":           tftypes.NewValue(ipsType, nil),
		"timeouts":      tftypes.NewValue(timeoutsType, nil),
	})
}
