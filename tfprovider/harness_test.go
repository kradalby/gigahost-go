package tfprovider

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kradalby/gigahost-go/testhelper"
)

// The harness drives the real provider over protocol 6, in process, against a
// fake HTTP API. No credentials, no tofu binary, no TF_ACC, no network beyond
// loopback — so the whole resource state machine is reachable from
// `go test ./...` and gates every PR.
//
// It exists because the layer between "pure helper" and "spend real money" had
// no coverage, and that is where the defects were: unknowns leaking into
// state, plan/apply mismatches, perpetual diffs. Those all reproduce here in
// milliseconds.
//
// The acceptance suite in *_acc_test.go still owns end-to-end truth against the
// live API; this owns everything that can be decided without it.

// harness is one provider instance wired to one fake API.
type harness struct {
	t   *testing.T
	api *testhelper.Server
	srv tfprotov6.ProviderServer
	ctx context.Context //nolint:containedctx // test scaffold, scoped to the harness
}

// newHarness builds a configured provider pointed at a fresh fake API on
// httptest's in-memory network. The resources run their shipping poll
// cadences; tests that reach a poll loop wrap themselves in synctest.Test so
// the clock is fake and the walk is still instant.
func newHarness(t *testing.T) *harness {
	t.Helper()

	api := testhelper.NewServer(t)
	ctx := context.Background()

	p := New("test", withHTTPClient(api.Client()))

	srv := providerserver.NewProtocol6(p())()

	h := &harness{t: t, api: api, srv: srv, ctx: ctx}
	h.configure()

	return h
}

// providerObjectType is the provider block's own schema type.
func (h *harness) providerObjectType() tftypes.Object {
	h.t.Helper()

	resp, err := h.srv.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		h.t.Fatalf("GetProviderSchema: %v", err)
	}

	return resp.Provider.ValueType().(tftypes.Object) //nolint:forcetypeassert // schema is always an object
}

// resourceObjectType returns the tftypes object for a resource's schema, which
// every value the harness builds must conform to.
func (h *harness) resourceObjectType(name string) tftypes.Object {
	h.t.Helper()

	resp, err := h.srv.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		h.t.Fatalf("GetProviderSchema: %v", err)
	}

	sch, ok := resp.ResourceSchemas[name]
	if !ok {
		h.t.Fatalf("no schema for resource %q", name)
	}

	return sch.ValueType().(tftypes.Object) //nolint:forcetypeassert // schema is always an object
}

// configure points the provider at the fake API with a dummy token.
func (h *harness) configure() {
	h.t.Helper()

	objType := h.providerObjectType()
	cfg := mkObject(objType, map[string]tftypes.Value{
		"token":    tfStr("test-token"),
		"base_url": tfStr(h.api.URL()),
	})

	dv, err := tfprotov6.NewDynamicValue(objType, cfg)
	if err != nil {
		h.t.Fatalf("encode provider config: %v", err)
	}

	resp, err := h.srv.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{Config: &dv})
	if err != nil {
		h.t.Fatalf("ConfigureProvider: %v", err)
	}

	failOnError(h.t, "ConfigureProvider", resp.Diagnostics)
}

// planResult is what a PlanResourceChange produced.
type planResult struct {
	t               *testing.T
	name            string
	Planned         map[string]tftypes.Value
	plannedValue    tftypes.Value
	RequiresReplace []string
	Diagnostics     []*tfprotov6.Diagnostic
}

// applyResult is what an ApplyResourceChange produced.
type applyResult struct {
	t           *testing.T
	name        string
	State       map[string]tftypes.Value
	stateValue  tftypes.Value
	Diagnostics []*tfprotov6.Diagnostic
}

// plan runs PlanResourceChange. prior may be a null object for a create;
// config carries what the practitioner wrote.
func (h *harness) plan(name string, prior, config tftypes.Value) planResult {
	h.t.Helper()

	objType := h.resourceObjectType(name)
	proposed := proposedNew(h.t, objType, prior, config)

	req := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         name,
		PriorState:       h.dyn(objType, prior),
		ProposedNewState: h.dyn(objType, proposed),
		Config:           h.dyn(objType, config),
	}

	resp, err := h.srv.PlanResourceChange(h.ctx, req)
	if err != nil {
		h.t.Fatalf("PlanResourceChange(%s): %v", name, err)
	}

	res := planResult{t: h.t, name: name, Diagnostics: resp.Diagnostics}

	// AttributePath.String() renders as AttributeName("size"); tests name
	// attributes, so reduce a root path to its bare name.
	for _, p := range resp.RequiresReplace {
		steps := p.Steps()
		if len(steps) == 1 {
			if name, ok := steps[0].(tftypes.AttributeName); ok {
				res.RequiresReplace = append(res.RequiresReplace, string(name))

				continue
			}
		}

		res.RequiresReplace = append(res.RequiresReplace, p.String())
	}

	if resp.PlannedState != nil {
		res.plannedValue = h.unmarshal(objType, resp.PlannedState)
		res.Planned = asMap(h.t, res.plannedValue)
	}

	return res
}

// apply runs ApplyResourceChange and asserts the framework contract Terraform
// core would otherwise enforce: the resulting state must be wholly known, and
// every attribute the plan pinned to a concrete value must come back equal.
//
// Asserting that here is the point of the harness. It turns "provider produced
// inconsistent result after apply" — a class of bug that previously only
// surfaced during a real apply against the live API — into an ordinary unit
// test failure naming the attribute.
func (h *harness) apply(name string, prior, planned, config tftypes.Value) applyResult {
	h.t.Helper()

	objType := h.resourceObjectType(name)

	req := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     name,
		PriorState:   h.dyn(objType, prior),
		PlannedState: h.dyn(objType, planned),
		Config:       h.dyn(objType, config),
	}

	resp, err := h.srv.ApplyResourceChange(h.ctx, req)
	if err != nil {
		h.t.Fatalf("ApplyResourceChange(%s): %v", name, err)
	}

	res := applyResult{t: h.t, name: name, Diagnostics: resp.Diagnostics}

	if resp.NewState != nil {
		res.stateValue = h.unmarshal(objType, resp.NewState)
		res.State = asMap(h.t, res.stateValue)
	}

	if !hasError(resp.Diagnostics) && res.State != nil {
		res.assertWhollyKnown()
		res.assertMatchesPlan(asMap(h.t, planned))
	}

	return res
}

// importState runs ImportResourceState and returns the imported objects.
func (h *harness) importState(name, id string) []map[string]tftypes.Value {
	h.t.Helper()

	objType := h.resourceObjectType(name)

	resp, err := h.srv.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{
		TypeName: name,
		ID:       id,
	})
	if err != nil {
		h.t.Fatalf("ImportResourceState(%s, %s): %v", name, id, err)
	}

	failOnError(h.t, "ImportResourceState", resp.Diagnostics)

	out := make([]map[string]tftypes.Value, 0, len(resp.ImportedResources))

	for _, imported := range resp.ImportedResources {
		out = append(out, asMap(h.t, h.unmarshal(objType, imported.State)))
	}

	return out
}

// read runs ReadResource. A nil return means the provider dropped the resource
// from state, which is how it reports "gone upstream".
func (h *harness) read(name string, state tftypes.Value) map[string]tftypes.Value {
	h.t.Helper()

	objType := h.resourceObjectType(name)

	resp, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     name,
		CurrentState: h.dyn(objType, state),
	})
	if err != nil {
		h.t.Fatalf("ReadResource(%s): %v", name, err)
	}

	failOnError(h.t, "ReadResource", resp.Diagnostics)

	got := h.unmarshal(objType, resp.NewState)
	if got.IsNull() {
		return nil
	}

	return asMap(h.t, got)
}

// tryRead is read() without the fatal: it returns whatever came back,
// diagnostics included, for callers that treat an error as one acceptable
// outcome among several.
func (h *harness) tryRead(name string, state tftypes.Value) (map[string]tftypes.Value, []*tfprotov6.Diagnostic) {
	h.t.Helper()

	objType := h.resourceObjectType(name)

	resp, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     name,
		CurrentState: h.dyn(objType, state),
	})
	if err != nil {
		h.t.Fatalf("ReadResource(%s): %v", name, err)
	}

	if resp.NewState == nil {
		return nil, resp.Diagnostics
	}

	got := h.unmarshal(objType, resp.NewState)
	if got.IsNull() {
		return nil, resp.Diagnostics
	}

	return asMap(h.t, got), resp.Diagnostics
}

func (h *harness) dyn(objType tftypes.Object, v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()

	dv, err := tfprotov6.NewDynamicValue(objType, v)
	if err != nil {
		h.t.Fatalf("encode dynamic value: %v", err)
	}

	return &dv
}

func (h *harness) unmarshal(objType tftypes.Object, dv *tfprotov6.DynamicValue) tftypes.Value {
	h.t.Helper()

	v, err := dv.Unmarshal(objType)
	if err != nil {
		h.t.Fatalf("decode dynamic value: %v", err)
	}

	return v
}

// assertWhollyKnown fails when any attribute is still unknown after apply.
// Terraform core rejects such a state with "provider returned invalid result
// object after apply", which reads as a provider bug to the practitioner.
func (r applyResult) assertWhollyKnown() {
	r.t.Helper()

	for k, v := range r.State {
		if !v.IsKnown() {
			r.t.Errorf("%s: attribute %q is unknown after apply; Terraform rejects this state", r.name, k)
		}
	}
}

// assertMatchesPlan fails when apply returned a different value than the plan
// promised for a known attribute — "provider produced inconsistent result
// after apply".
func (r applyResult) assertMatchesPlan(planned map[string]tftypes.Value) {
	r.t.Helper()

	for k, want := range planned {
		if !want.IsKnown() {
			// Unknown is the only wildcard: it promised a value, not a
			// particular one. Every other planned value, null included, must
			// come back exactly as promised.
			continue
		}

		got, ok := r.State[k]
		if !ok {
			continue
		}

		if !got.Equal(want) {
			r.t.Errorf("%s: inconsistent result after apply: %q was %v in the plan, but %v in state",
				r.name, k, want, got)
		}
	}
}

// HasError reports whether the plan produced an error diagnostic.
func (r planResult) HasError() bool { return hasError(r.Diagnostics) }

// HasError reports whether the apply produced an error diagnostic.
func (r applyResult) HasError() bool { return hasError(r.Diagnostics) }

// ErrorText joins every error diagnostic, for assertions on the message.
func (r planResult) ErrorText() string {
	return diagText(r.Diagnostics, tfprotov6.DiagnosticSeverityError)
}

// ErrorText joins every error diagnostic, for assertions on the message.
func (r applyResult) ErrorText() string {
	return diagText(r.Diagnostics, tfprotov6.DiagnosticSeverityError)
}

// WarningText joins every warning diagnostic.
func (r planResult) WarningText() string {
	return diagText(r.Diagnostics, tfprotov6.DiagnosticSeverityWarning)
}

// WarningText joins every warning diagnostic.
func (r applyResult) WarningText() string {
	return diagText(r.Diagnostics, tfprotov6.DiagnosticSeverityWarning)
}

// Unknown reports whether the planned value for attr is unknown.
func (r planResult) Unknown(attr string) bool {
	v, ok := r.Planned[attr]

	return ok && !v.IsKnown()
}

// Null reports whether the planned value for attr is known and null.
func (r planResult) Null(attr string) bool {
	v, ok := r.Planned[attr]

	return ok && v.IsKnown() && v.IsNull()
}

// Replaces reports whether the plan requires replacing because of attr.
func (r planResult) Replaces(attr string) bool {
	return slices.Contains(r.RequiresReplace, attr)
}

// assertElapsed pins how much synthetic time a bubbled call consumed.
//
// Callers state want in terms of the cadence constants, so this pins loop
// *structure*, not their magnitude: how many waits the loop performed. A loop
// that dropped its wait, retried the wrong number of times, or kept waiting
// after upstream recovered all move the elapsed time and fail here. Nothing
// else in the suite observes the delay at all.
//
// It deliberately does not re-assert the constants' values — that would just
// restate the same expression on both sides. TestPollCadencesAreProduction
// covers magnitude instead.
//
// Inside a synctest bubble the comparison is exact rather than approximate,
// because the only time that passes is time the loop asked to sleep.
func assertElapsed(t *testing.T, start time.Time, want time.Duration) {
	t.Helper()

	if got := time.Since(start); got != want {
		t.Errorf("waited %s of synthetic time, want exactly %s", got, want)
	}
}

// TestPollCadencesAreProduction guards the property the deleted tuning seam
// used to destroy: these are the cadences that ship, not values shrunk to keep
// a wall-clock test fast.
//
// It is the half assertElapsed cannot cover. Tests now reach the poll loops
// through the same constants production uses, so the only thing stopping
// someone from shrinking one to speed a test up is that doing so is a visible
// change to shipping behaviour. This makes it a failing one. Synthetic time
// means a realistic cadence costs the suite nothing.
func TestPollCadencesAreProduction(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		got  time.Duration
		min  time.Duration
	}{
		{"deployPollInterval", deployPollInterval, time.Second},
		{"serverAbsenceDelay", serverAbsenceDelay, time.Second},
		{"reinstallTimeout", reinstallTimeout, time.Minute},
		{"deployReadyTimeout", deployReadyTimeout, time.Minute},
	} {
		if c.got < c.min {
			t.Errorf("%s = %s, want at least %s; a sub-%s cadence is a test "+
				"fiction, and polling that hard would hammer the live API",
				c.name, c.got, c.min, c.min)
		}
	}
}
