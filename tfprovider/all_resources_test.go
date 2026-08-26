package tfprovider

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A sweep across every registered resource, rather than a bespoke fixture per
// resource. It cannot assert what each one means, but it can assert the
// contracts every one of them shares — and those are exactly where the
// per-resource bugs turned up.

// resourceTypeNames returns every gigahost_* resource the provider registers,
// so a resource added later is swept automatically instead of being forgotten.
func resourceTypeNames(t *testing.T) []string {
	t.Helper()

	ctx := context.Background()
	p := New("test")().(*gigahostProvider) //nolint:forcetypeassert // our own type

	var names []string

	for _, newResource := range p.Resources(ctx) {
		var resp resource.MetadataResponse

		newResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "gigahost"}, &resp)
		names = append(names, resp.TypeName)
	}

	return names
}

// TestEveryResourceIsRegisteredWithASchema is the cheapest possible guard
// against a resource being added to the list but not wired up.
func TestEveryResourceIsRegisteredWithASchema(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	names := resourceTypeNames(t)
	if len(names) < 15 {
		t.Fatalf("only %d resources registered; the sweep is not covering the provider", len(names))
	}

	for _, name := range names {
		objType := h.resourceObjectType(name)

		if _, ok := objType.AttributeTypes["id"]; !ok {
			t.Errorf("%s has no id attribute; import and state addressing both need one", name)
		}
	}
}

// TestEveryResourceHandlesAGoneUpstreamRead is the drift contract. When the
// API no longer knows about an object, Read must either drop it from state so
// the next plan recreates it, or report a real error — never return a state
// carrying unknowns, and never panic.
//
// Several resources reach this path on a single missed read, which is why it
// is worth asserting for all of them rather than the two that were reviewed.
func TestEveryResourceHandlesAGoneUpstreamRead(t *testing.T) {
	t.Parallel()

	// Minimal prior state per resource: whatever Read needs to address the
	// object. Anything omitted is null.
	priors := map[string]map[string]tftypes.Value{
		"gigahost_dns_zone":                {"id": tfStr("5000"), "name": tfStr("example.no")},
		"gigahost_dns_record":              {"id": tfStr("5000/r1"), "zone_id": tfStr("5000"), "record_id": tfStr("r1")},
		"gigahost_dns_redirect":            {"id": tfStr("5000/@"), "zone_id": tfStr("5000"), "source": tfStr("@")},
		"gigahost_dns_dnssec":              {"id": tfStr("5000"), "zone_id": tfStr("5000")},
		"gigahost_dns_ptr_zone":            {"id": tfStr("6000"), "zone_name": tfStr("2.0.192.in-addr.arpa")},
		"gigahost_dns_nameservers":         {"id": tfStr("5000"), "zone_id": tfStr("5000")},
		"gigahost_dns_external_ds_records": {"id": tfStr("5000"), "zone_id": tfStr("5000")},
		"gigahost_bgp_asn":                 {"id": tfStr("1"), "asn": tfStr("AS64500")},
		"gigahost_bgp_session":             {"id": tfStr("77"), "asn_id": tfStr("5")},
		"gigahost_server":                  {"id": tfStr("18394")},
		"gigahost_server_ipv4":             {"id": tfStr("9"), "server_id": tfStr("18394")},
		"gigahost_server_snapshot":         {"id": tfStr("18394/7"), "server_id": tfStr("18394"), "snapshot_id": tfStr("7")},
		"gigahost_server_name":             {"id": tfStr("18394"), "server_id": tfStr("18394")},
		"gigahost_server_rdns":             {"id": tfStr("18394/9"), "server_id": tfStr("18394"), "ip_id": tfStr("9")},
		"gigahost_account_ssh_key":         {"id": tfStr("2257")},
		"gigahost_account_api_key":         {"id": tfStr("3")},
	}

	for _, name := range resourceTypeNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				prior, ok := priors[name]
				if !ok {
					t.Fatalf("%s is registered but the sweep has no prior state for it; add one", name)
				}

				h := newHarness(t)
				objType := h.resourceObjectType(name)

				// Everything is gone.
				for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
					h.api.Route(method, "/*").Respond(http.StatusNotFound,
						`{"meta":{"status":404,"status_message":"404 Not Found","message":"Not found."}}`)
				}

				state := mkObject(objType, prior)

				got, _ := h.tryRead(name, state)

				// Two outcomes are legitimate here, and which one is right is a
				// per-resource judgement rather than a rule. Dropping the object
				// from state suits a definitive 404 on the object itself. Keeping
				// it and reporting an error suits a 404 on a collection endpoint,
				// where the parent may be gone or the API may simply be unwell —
				// and guessing "deleted" would destroy and recreate live
				// infrastructure.
				//
				// What is never acceptable, for any resource, is a panic or a
				// state carrying unknowns: Terraform rejects the latter outright
				// and reports it to the practitioner as a provider bug.
				for attr, v := range got {
					if !v.IsKnown() {
						t.Errorf("%s: Read left %q unknown after the object went away", name, attr)
					}
				}
			})
		})
	}
}
