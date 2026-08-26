package client_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kradalby/gigahost-go/client"
)

func TestDNSZoneLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// List initially returns one zone (from fixture).
	srv.Expect("GET", "/dns/zones").
		RespondFixture(t, "testdata/dns/list_zones.json")

	// Create a new zone. POST returns an empty data array, so the client
	// resolves the new ID by listing zones again and matching the name.
	srv.Expect("POST", "/dns/zones").
		WithJSON(`{"zone_name":"new-zone.no"}`).
		RespondFixture(t, "testdata/dns/create_zone.json")

	srv.Expect("GET", "/dns/zones").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":[{"zone_id":"456","zone_name":"new-zone.no","zone_type":"NATIVE"}]}`)

	// Delete the new zone.
	srv.Expect("DELETE", "/dns/zones/456").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"Zone deleted successfully."}}`)

	ctx := context.Background()

	// Step 1: list zones.
	zones, err := c.DNS.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}

	if len(zones) != 1 {
		t.Fatalf("want 1 zone, got %d", len(zones))
	}

	z := zones[0]

	if z.Name != "example.no" {
		t.Errorf("Name = %q", z.Name)
	}

	if !z.Active {
		t.Error("Active should be true")
	}

	if !z.Protected {
		t.Error("Protected should be true")
	}

	if z.Type != client.ZoneTypeNative {
		t.Errorf("Type = %q", z.Type)
	}

	wantExpiry := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	if !z.ExpiryDate.Equal(wantExpiry) {
		t.Errorf("ExpiryDate = %v, want %v", z.ExpiryDate, wantExpiry)
	}

	wantUpdated := time.Unix(1700000000, 0).UTC()
	if !z.UpdatedAt.Equal(wantUpdated) {
		t.Errorf("UpdatedAt = %v, want %v", z.UpdatedAt, wantUpdated)
	}

	// Step 2: create.
	created, err := c.DNS.CreateZone(ctx, client.CreateZoneRequest{Name: "new-zone.no"})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	if created.ID != "456" {
		t.Errorf("ID = %q", created.ID)
	}

	// Step 3: delete.
	if err := c.DNS.DeleteZone(ctx, created.ID); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
}

func TestDNSRecordLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// List records.
	srv.Expect("GET", "/dns/zones/123/records").
		RespondFixture(t, "testdata/dns/list_records.json")

	// Create record.
	srv.Expect("POST", "/dns/zones/123/records").
		WithJSON(`{"record_name":"www","record_type":"A","record_value":"1.2.3.4","record_ttl":3600}`).
		Respond(http.StatusCreated, `{"meta":{"status":201,"status_message":"201 Created","message":"Record created successfully."}}`)

	// Update record.
	srv.Expect("PUT", "/dns/zones/123/records/def456").
		WithJSON(`{"record_value":"5.6.7.8"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Delete record. `value` is not optional: without it the API deletes
	// every record sharing the name and type (upstream B21).
	srv.Expect("DELETE", "/dns/zones/123/records/def456").
		WithQueryPairs("name", "www", "type", "A", "value", "192.0.2.1").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	records, err := c.DNS.ListRecords(ctx, "123")
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("want 3 records, got %d", len(records))
	}

	// Find the MX record and check its priority.
	var mx *client.DNSRecord

	for i := range records {
		if records[i].Type == client.RecordTypeMX {
			mx = &records[i]

			break
		}
	}

	if mx == nil {
		t.Fatal("no MX record found")
	}

	if mx.Priority == nil || *mx.Priority != 10 {
		t.Errorf("Priority = %v, want *10", mx.Priority)
	}

	if err := c.DNS.CreateRecord(ctx, "123", client.CreateRecordRequest{
		Name:  "www",
		Type:  client.RecordTypeA,
		Value: "1.2.3.4",
		TTL:   3600,
	}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	if err := c.DNS.UpdateRecord(ctx, "123", "def456", client.UpdateRecordRequest{
		Value: "5.6.7.8",
	}); err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}

	if err := c.DNS.DeleteRecord(ctx, "123", "def456", client.DeleteRecordRequest{
		Name:  "www",
		Type:  client.RecordTypeA,
		Value: "192.0.2.1",
	}); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}

	// An empty value must be refused rather than sent: the API would read it
	// as "delete the whole RRset" (upstream B21).
	if err := c.DNS.DeleteRecord(ctx, "123", "def456", client.DeleteRecordRequest{
		Name: "www",
		Type: client.RecordTypeA,
	}); err == nil {
		t.Fatal("DeleteRecord with empty value: want error, got nil")
	}
}

// TestDNSDeleteRecordMXWireValue pins the one type whose delete matcher does
// not accept the value ListRecords reports: MX needs the backend's stored
// content, "<priority> <target>." (upstream B21). Sending the listed value
// instead makes the API 404, and dropping `value` entirely makes it delete
// the whole RRset.
func TestDNSDeleteRecordMXWireValue(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("DELETE", "/dns/zones/123/records/mx1").
		WithQueryPairs("name", "@", "type", "MX", "value", "10 mail.example.com.").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	priority := 10
	if err := c.DNS.DeleteRecord(context.Background(), "123", "mx1", client.DeleteRecordRequest{
		Name:     "@",
		Type:     client.RecordTypeMX,
		Value:    "mail.example.com",
		Priority: &priority,
	}); err != nil {
		t.Fatalf("DeleteRecord MX: %v", err)
	}
}

// TestDNSDeleteRecordRequiresMXPriority pins the guard on the one type whose
// delete matcher includes the priority. A nil Priority used to render
// "0 <target>.", a well-formed value naming a different record, so the delete
// silently matched nothing and the destroy failed with a bare 404. Guessing is
// the wrong default on an endpoint whose failure mode is deleting the wrong
// thing — refuse instead, the way an empty Value is refused.
func TestDNSDeleteRecordRequiresMXPriority(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	err := c.DNS.DeleteRecord(context.Background(), "123", "mx1", client.DeleteRecordRequest{
		Name:  "@",
		Type:  client.RecordTypeMX,
		Value: "mail.example.com",
		// Priority deliberately omitted.
	})
	if err == nil {
		t.Fatal("DeleteRecord for MX without Priority: want an error, got nil")
	}

	if !strings.Contains(err.Error(), "Priority") {
		t.Errorf("error %q should name the missing Priority", err)
	}
}

func TestDNSRedirectLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/dns/zones/123/redirect").
		WithJSON(`{"source":"@","target_url":"https://www.example.com"}`).
		Respond(http.StatusCreated, `{"meta":{"status":201,"status_message":"201 Created"}}`)

	srv.Expect("PUT", "/dns/zones/123/redirect").
		WithJSON(`{"source":"@","target_url":"https://new.example.com"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	srv.Expect("DELETE", "/dns/zones/123/redirect").
		WithQueryPairs("source", "@").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	if err := c.DNS.CreateRedirect(ctx, "123", client.CreateRedirectRequest{
		Source:    "@",
		TargetURL: "https://www.example.com",
	}); err != nil {
		t.Fatalf("CreateRedirect: %v", err)
	}

	if err := c.DNS.UpdateRedirect(ctx, "123", "@", "https://new.example.com"); err != nil {
		t.Fatalf("UpdateRedirect: %v", err)
	}

	if err := c.DNS.DeleteRedirect(ctx, "123", "@"); err != nil {
		t.Fatalf("DeleteRedirect: %v", err)
	}
}

func TestDNSCheckDomain(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/domains/check/example.no").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"domain":"example.no","available":true,"reason":""}}`)

	ctx := context.Background()

	check, err := c.DNS.CheckDomain(ctx, "example.no")
	if err != nil {
		t.Fatalf("CheckDomain: %v", err)
	}

	if !check.Available {
		t.Error("want Available=true")
	}
}

func TestDNSDNSSECToggle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/dns/zones/123/dnssec").
		WithJSON(`{"enable":1}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	srv.Expect("PUT", "/dns/zones/123/dnssec").
		WithJSON(`{"enable":0}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	if err := c.DNS.SetDNSSEC(ctx, "123", true); err != nil {
		t.Fatalf("SetDNSSEC(on): %v", err)
	}

	if err := c.DNS.SetDNSSEC(ctx, "123", false); err != nil {
		t.Fatalf("SetDNSSEC(off): %v", err)
	}
}
