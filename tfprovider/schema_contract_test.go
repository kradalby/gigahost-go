package tfprovider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These pin schema-shape contracts that only show up at apply time. Each one
// caused, or would cause, a failure a practitioner reads as a provider bug.

// TestDNSRecordCreateToleratesMixedCaseName covers a live API behaviour: DNS
// names are case-insensitive and Gigahost stores them lower-cased. A record
// configured as "WWW" comes back as "www", and a byte-exact lookup after
// create reports "record not found after create" — for a record that was in
// fact created, and is now live but outside Terraform state.
func TestDNSRecordCreateToleratesMixedCaseName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_record")

	zone := &zoneRecords{}
	wireRecordRoutes(h, "5000", zone)

	// The API lower-cases whatever name it is given.
	h.api.Route(http.MethodPost, "/dns/zones/5000/records").
		RespondWith(func(_ *http.Request, _ int) (int, string) {
			zone.add("r1", "www", "A", "192.0.2.1", 300, nil)

			return http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`
		})

	config := recordState(objType, "5000", "", "WWW", "A", "192.0.2.1")

	planned := h.plan("gigahost_dns_record", nullObject(objType), config)
	if planned.HasError() {
		t.Fatalf("plan: %s", planned.ErrorText())
	}

	res := h.apply("gigahost_dns_record", nullObject(objType), planned.plannedValue, config)
	if res.HasError() {
		t.Fatalf("creating a record with an upper-case name failed: %s\n"+
			"the record exists upstream but is not in state", res.ErrorText())
	}

	if got := str(res.State, "record_id"); got != "r1" {
		t.Errorf("record_id = %q, want the created record's id", got)
	}
}

// TestDNSZoneTypeRequiresReplace pins that a zone type change replaces. There
// is no API to change it — Update only re-reads the zone — so planning it in
// place would do nothing and then fail the consistency check.
func TestDNSZoneTypeRequiresReplace(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_zone")

	prior := mkObject(objType, map[string]tftypes.Value{
		"id":   tfStr("5000"),
		"name": tfStr("example.no"),
		"type": tfStr("NATIVE"),
	})
	config := mkObject(objType, map[string]tftypes.Value{
		"name": tfStr("example.no"),
		"type": tfStr("MASTER"),
	})

	res := h.plan("gigahost_dns_zone", prior, config)
	if res.HasError() {
		t.Fatalf("plan: %s", res.ErrorText())
	}

	if !res.Replaces("type") {
		t.Error("changing a zone's type planned an in-place update; the API has no " +
			"way to perform it, so the apply would silently do nothing and then fail")
	}
}

// TestBGPSessionOptionalBoolsAreComputed guards the shape that made the
// resource unusable: Read writes the API's answer into default_route and
// redundant, so leaving them out of the config failed the first apply with
// "was null, but now false". Worse, once state held false against a null
// config, RequiresReplace tore the session down on every later plan.
func TestBGPSessionOptionalBoolsAreComputed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_bgp_session")

	prior := mkObject(objType, map[string]tftypes.Value{
		"id":            tfStr("77"),
		"asn_id":        tfStr("5"),
		"ipv4_ip_id":    tfStr("9"),
		"default_route": tfBool(false),
		"redundant":     tfBool(false),
		"status":        tfStr("active"),
	})

	// The config omits both bools, exactly as the documented example does.
	config := mkObject(objType, map[string]tftypes.Value{
		"asn_id":     tfStr("5"),
		"ipv4_ip_id": tfStr("9"),
	})

	res := h.plan("gigahost_bgp_session", prior, config)
	if res.HasError() {
		t.Fatalf("plan: %s", res.ErrorText())
	}

	for _, attr := range []string{"default_route", "redundant"} {
		if res.Replaces(attr) {
			t.Errorf("omitting %q from config planned a replacement; the session "+
				"would be torn down and rebuilt on every apply", attr)
		}
	}
}

// TestServerOSChangeCombinedWithReplaceDoesNotWarn covers a spurious warning:
// when the os changes at the same time as a replacing attribute, the server is
// rebuilt, so the in-place "ALL DATA ON DISK IS WIPED" warning is misleading —
// it describes a reinstall that will not happen.
func TestServerOSChangeCombinedWithReplaceDoesNotWarn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_server")

	prior := managedServerState(objType, "debian-11")
	// Both the size and the os change: size forces replacement.
	config := mkObject(objType, map[string]tftypes.Value{
		"type": tfStr("value"), "size": tfStr("4c-8gb-80gb"), "os": tfStr("debian-12"),
	})

	res := h.plan("gigahost_server", prior, config)
	if res.HasError() {
		t.Fatalf("plan: %s", res.ErrorText())
	}

	if !res.Replaces("size") {
		t.Fatal("changing size must replace the server")
	}

	if res.WarningText() != "" {
		t.Errorf("planned a replacement but still warned about an in-place reinstall: %q",
			res.WarningText())
	}
}
