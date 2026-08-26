package tfprovider

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// zoneRecords is a tiny in-test store standing in for one DNS zone. It exists
// so a lifecycle walk can assert the property that actually matters — what is
// left in the zone after each operation — rather than asserting which HTTP
// calls the provider happened to make.
//
// This is deliberately per-test and about forty lines. It is not a fake
// Gigahost API; end-to-end truth stays with the live acceptance suite.
type zoneRecords struct {
	mu   sync.Mutex
	recs []map[string]any
}

func (z *zoneRecords) add(id, name, recType, value string, ttl int, priority any) {
	z.mu.Lock()
	defer z.mu.Unlock()

	z.recs = append(z.recs, map[string]any{
		"record_id": id, "record_name": name, "record_type": recType,
		"record_value": value, "record_ttl": ttl, "record_priority": priority,
	})
}

// deleteMatching removes records the way the API does: on name+type+value.
func (z *zoneRecords) deleteMatching(name, recType, value string) int {
	z.mu.Lock()
	defer z.mu.Unlock()

	kept := z.recs[:0]
	removed := 0

	for _, r := range z.recs {
		if r["record_name"] == name && r["record_type"] == recType && r["record_value"] == value {
			removed++

			continue
		}

		kept = append(kept, r)
	}

	z.recs = kept

	return removed
}

func (z *zoneRecords) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()

	return len(z.recs)
}

// listJSON renders the store as a /dns/zones/{id}/records response.
func (z *zoneRecords) listJSON() string {
	z.mu.Lock()
	defer z.mu.Unlock()

	var b strings.Builder

	b.WriteString(`{"meta":{"status":200,"status_message":"200 OK"},"data":[`)

	for i, r := range z.recs {
		if i > 0 {
			b.WriteString(",")
		}

		priority := "null"
		if p, ok := r["record_priority"].(int); ok {
			priority = strconv.Itoa(p)
		}

		fmt.Fprintf(&b, `{"record_id":%q,"record_name":%q,"record_type":%q,"record_value":%q,"record_ttl":%d,"record_priority":%s}`,
			r["record_id"], r["record_name"], r["record_type"], r["record_value"], r["record_ttl"], priority)
	}

	b.WriteString(`]}`)

	return b.String()
}

// wireRecordRoutes points the harness's fake API at the store.
func wireRecordRoutes(h *harness, zoneID string, z *zoneRecords) {
	base := "/dns/zones/" + zoneID + "/records"

	h.api.Route(http.MethodGet, base).
		RespondWith(func(_ *http.Request, _ int) (int, string) {
			return http.StatusOK, z.listJSON()
		})

	h.api.Route(http.MethodDelete, base+"/*").
		RespondWith(func(r *http.Request, _ int) (int, string) {
			q := r.URL.Query()
			if z.deleteMatching(q.Get("name"), q.Get("type"), q.Get("value")) == 0 {
				return http.StatusNotFound,
					`{"meta":{"status":404,"status_message":"404 Not Found","message":"Record not found."}}`
			}

			return http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`
		})
}

// recordState builds a gigahost_dns_record state/config object.
func recordState(objType tftypes.Object, zoneID, recordID, name, recType, value string) tftypes.Value {
	set := map[string]tftypes.Value{
		"zone_id": tfStr(zoneID),
		"name":    tfStr(name),
		"type":    tfStr(recType),
		"value":   tfStr(value),
	}

	if recordID != "" {
		set["record_id"] = tfStr(recordID)
		set["id"] = tfStr(zoneID + "/" + recordID)
	}

	return mkObject(objType, set)
}

// TestDNSRecordDestroyLeavesSiblings is the regression guard for the data-loss
// bug: deleting one record of an RRset used to take every record sharing its
// name and type with it. Nothing at any layer asserted the surviving sibling
// until now.
func TestDNSRecordDestroyLeavesSiblings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_record")

	zone := &zoneRecords{}
	zone.add("r1", "www", "A", "192.0.2.1", 300, nil)
	zone.add("r2", "www", "A", "192.0.2.2", 300, nil)
	zone.add("r3", "www", "A", "192.0.2.3", 300, nil)
	wireRecordRoutes(h, "5000", zone)

	state := recordState(objType, "5000", "r2", "www", "A", "192.0.2.2")

	res := h.apply("gigahost_dns_record", state, nullObject(objType), nullObject(objType))
	if res.HasError() {
		t.Fatalf("destroy: %s", res.ErrorText())
	}

	if got := zone.count(); got != 2 {
		t.Errorf("zone holds %d records after destroying one of three, want 2 "+
			"(the RRset was wiped — upstream B21 regression)", got)
	}

	for _, want := range []string{"192.0.2.1", "192.0.2.3"} {
		if !strings.Contains(zone.listJSON(), want) {
			t.Errorf("sibling %s was destroyed by a single-record delete", want)
		}
	}

	if strings.Contains(zone.listJSON(), "192.0.2.2") {
		t.Error("the targeted record survived its own destroy")
	}
}

// TestDNSRecordDestroyIsIdempotent covers the case a user hits after the old
// RRset bug: the record is already gone upstream, and destroy must succeed
// rather than wedging the resource in state forever.
func TestDNSRecordDestroyIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_record")

	zone := &zoneRecords{} // deliberately empty: the record is already gone
	wireRecordRoutes(h, "5000", zone)

	state := recordState(objType, "5000", "r1", "www", "A", "192.0.2.1")

	res := h.apply("gigahost_dns_record", state, nullObject(objType), nullObject(objType))
	if res.HasError() {
		t.Errorf("destroying an already-absent record failed: %s\n"+
			"terraform destroy can never succeed for anyone whose record was "+
			"removed out of band", res.ErrorText())
	}
}

// TestDNSRecordReadDropsDeletedRecord pins the drift path: a record removed
// out of band must leave state so the next plan recreates it.
func TestDNSRecordReadDropsDeletedRecord(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_record")

	zone := &zoneRecords{}
	wireRecordRoutes(h, "5000", zone)

	state := recordState(objType, "5000", "r1", "www", "A", "192.0.2.1")

	if got := h.read("gigahost_dns_record", state); got != nil {
		t.Errorf("Read kept a record that is gone upstream: %v", got)
	}
}

// TestDNSRecordReadRefreshesValue proves drift in the record's value is
// detected rather than silently kept.
func TestDNSRecordReadRefreshesValue(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_dns_record")

	zone := &zoneRecords{}
	zone.add("r1", "www", "A", "192.0.2.99", 300, nil) // changed out of band
	wireRecordRoutes(h, "5000", zone)

	state := recordState(objType, "5000", "r1", "www", "A", "192.0.2.1")

	got := h.read("gigahost_dns_record", state)
	if got == nil {
		t.Fatal("Read dropped a record that exists")
	}

	if v := str(got, "value"); v != "192.0.2.99" {
		t.Errorf("value = %q after out-of-band change, want the live value 192.0.2.99", v)
	}
}
