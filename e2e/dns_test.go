//go:build e2e

package e2e

import (
	"context"
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// findRecord returns a pointer to the first record in the zone matching the
// given type and value, or nil. Matching by (type,value) is robust to however
// the API normalises record names.
func findRecord(t *testing.T, c *gigahost.Client, zoneID string, typ gigahost.RecordType, value string) *gigahost.DNSRecord {
	t.Helper()

	recs, err := c.DNS.ListRecords(context.Background(), zoneID)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}

	for i := range recs {
		if recs[i].Type == typ && recs[i].Value == value {
			return &recs[i]
		}
	}

	return nil
}

// TestDNSZoneAndRecords creates a zone, adds records of several types, verifies
// them live, updates one, then tears everything down and confirms removal.
func TestDNSZoneAndRecords(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	zoneName := uniqueName("dns") + ".com"

	created, err := c.DNS.CreateZone(ctx, gigahost.CreateZoneRequest{
		Name: zoneName,
		Type: gigahost.ZoneTypeNative,
	})
	if err != nil {
		skipIfForbidden(t, err)
		t.Fatalf("CreateZone: %v", err)
	}

	zoneID := created.ID
	if zoneID == "" {
		t.Fatal("CreateZone returned empty ID")
	}

	t.Cleanup(func() {
		_ = c.DNS.DeleteZone(context.Background(), zoneID)
	})

	// Records to create: name (relative), type, value, optional priority.
	prio := 10
	cases := []struct {
		name  string
		typ   gigahost.RecordType
		value string
		prio  *int
	}{
		// The API stores hostnames without a trailing dot, so match on that.
		{"www", gigahost.RecordTypeA, "192.0.2.10", nil},
		{"www", gigahost.RecordTypeAAAA, "2001:db8::10", nil},
		{"alias", gigahost.RecordTypeCNAME, zoneName, nil},
		{"@", gigahost.RecordTypeMX, "mail." + zoneName, &prio},
		{"txt", gigahost.RecordTypeTXT, "hello e2e", nil},
	}

	for _, tc := range cases {
		err := c.DNS.CreateRecord(ctx, zoneID, gigahost.CreateRecordRequest{
			Name:     tc.name,
			Type:     tc.typ,
			Value:    tc.value,
			TTL:      300,
			Priority: tc.prio,
		})
		if err != nil {
			t.Fatalf("CreateRecord %s %s=%s: %v", tc.typ, tc.name, tc.value, err)
		}
	}

	// Verify each record landed.
	for _, tc := range cases {
		if findRecord(t, c, zoneID, tc.typ, tc.value) == nil {
			t.Errorf("record %s %s=%s not found after create", tc.typ, tc.name, tc.value)
		}
	}

	// Update the A record's value and confirm.
	a := findRecord(t, c, zoneID, gigahost.RecordTypeA, "192.0.2.10")
	if a == nil {
		t.Fatal("A record vanished before update")
	}

	const newIP = "192.0.2.20"
	if err := c.DNS.UpdateRecord(ctx, zoneID, a.ID, gigahost.UpdateRecordRequest{
		Name:  a.Name,
		Type:  gigahost.RecordTypeA,
		Value: newIP,
		TTL:   300,
	}); err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}

	if findRecord(t, c, zoneID, gigahost.RecordTypeA, newIP) == nil {
		t.Errorf("A record not updated to %s", newIP)
	}

	// Delete every record we created (the updated A by its new value).
	recs, err := c.DNS.ListRecords(ctx, zoneID)
	if err != nil {
		t.Fatalf("ListRecords before delete: %v", err)
	}

	for _, r := range recs {
		// Leave SOA/NS and other default records to zone deletion.
		switch r.Type {
		case gigahost.RecordTypeA, gigahost.RecordTypeAAAA, gigahost.RecordTypeCNAME,
			gigahost.RecordTypeMX, gigahost.RecordTypeTXT:
			if err := c.DNS.DeleteRecord(ctx, zoneID, r.ID, gigahost.DeleteRecordRequest{
				Name:     r.Name,
				Type:     r.Type,
				Value:    r.Value,
				Priority: r.Priority,
			}); err != nil {
				t.Errorf("DeleteRecord %s %s: %v", r.Type, r.Name, err)
			}
		}
	}

	// Tear down the zone and confirm it is gone.
	if err := c.DNS.DeleteZone(ctx, zoneID); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}

	zones, err := c.DNS.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones after delete: %v", err)
	}

	for _, z := range zones {
		if z.ID == zoneID {
			t.Errorf("zone %s still present after delete", zoneID)
		}
	}
}
