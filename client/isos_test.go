package client_test

import (
	"context"
	"net/http"
	"testing"
)

// ISO payload synthesized from the API docs; the test account's hourly
// VPS rejects the ISO endpoints, so no captured live fixture exists.
func TestISOsList(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers/3523/isos").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":[{"iso_id":"42","cust_id":"1001","iso_url":"https://example.no/debian.iso","iso_name":"debian.iso","iso_hash":"abc123","iso_size":"412090368","iso_state":"ready","iso_mounted":"1"}]}`)

	isos, err := c.ISOs.List(context.Background(), "3523")
	if err != nil {
		t.Fatalf("ISOs.List: %v", err)
	}

	if len(isos) != 1 {
		t.Fatalf("len(isos) = %d, want 1", len(isos))
	}

	iso := isos[0]
	if iso.ID != "42" || iso.Name != "debian.iso" || iso.Size != 412090368 || !iso.Mounted {
		t.Errorf("ISO = %+v", iso)
	}

	if _, err := c.ISOs.List(context.Background(), ""); err == nil {
		t.Error("expected error for empty serverID")
	}
}

func TestISOsMount(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/servers/3523/isos").
		WithJSON(`{"iso_id":"42"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.ISOs.Mount(context.Background(), "3523", "42"); err != nil {
		t.Fatalf("ISOs.Mount: %v", err)
	}

	if err := c.ISOs.Mount(context.Background(), "", "42"); err == nil {
		t.Error("expected error for empty serverID")
	}

	if err := c.ISOs.Mount(context.Background(), "3523", ""); err == nil {
		t.Error("expected error for empty isoID")
	}
}
