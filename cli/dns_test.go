package cli

import (
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func TestMatchRecord(t *testing.T) {
	t.Parallel()

	prio := 10

	records := []gigahost.DNSRecord{
		{ID: "1", Name: "www", Type: gigahost.RecordTypeA, Value: "1.2.3.4", TTL: 3600},
		{ID: "2", Name: "mail", Type: gigahost.RecordTypeMX, Value: "mail.example.com", TTL: 3600, Priority: &prio},
		{ID: "3", Name: "@", Type: gigahost.RecordTypeA, Value: "5.6.7.8", TTL: 300},
	}

	t.Run("matches exact record", func(t *testing.T) {
		t.Parallel()

		row, found := matchRecord(records, "www", "A", "1.2.3.4")
		if !found {
			t.Fatal("expected to find record, got not found")
		}

		if row.ID != "1" {
			t.Errorf("got ID %q, want %q", row.ID, "1")
		}
	})

	t.Run("matches MX record with priority", func(t *testing.T) {
		t.Parallel()

		row, found := matchRecord(records, "mail", "MX", "mail.example.com")
		if !found {
			t.Fatal("expected to find record, got not found")
		}

		if row.ID != "2" {
			t.Errorf("got ID %q, want %q", row.ID, "2")
		}

		if row.Priority != "10" {
			t.Errorf("got Priority %q, want %q", row.Priority, "10")
		}
	})

	t.Run("no match on wrong value", func(t *testing.T) {
		t.Parallel()

		_, found := matchRecord(records, "www", "A", "9.9.9.9")
		if found {
			t.Fatal("expected no match, got match")
		}
	})

	t.Run("no match on wrong type", func(t *testing.T) {
		t.Parallel()

		_, found := matchRecord(records, "www", "AAAA", "1.2.3.4")
		if found {
			t.Fatal("expected no match, got match")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		_, found := matchRecord(nil, "www", "A", "1.2.3.4")
		if found {
			t.Fatal("expected no match, got match")
		}
	})
}
