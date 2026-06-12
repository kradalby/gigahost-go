package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

func TestBGPLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// Get current BGP state.
	srv.Expect("GET", "/bgp").
		RespondFixture(t, "testdata/bgp/get.json")

	// Submit a new ASN, accept "AS"-prefixed and confirm it's stripped.
	srv.Expect("POST", "/bgp/asn").
		WithJSON(`{"asn":"212345"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"ASN and LOA has been submitted for review."}}`)

	// Create a session.
	srv.Expect("POST", "/bgp/1/session").
		WithJSON(`{"redundant":1,"defaultroute":1,"ip_id_v4":"7795","ip_id_v6":"7796"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"BGP sessions has been created."}}`)

	// Delete a session.
	srv.Expect("DELETE", "/bgp/1/session").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"BGP sessions have been marked for deletion."}}`)

	ctx := context.Background()

	data, err := c.BGP.Get(ctx)
	if err != nil {
		t.Fatalf("BGP.Get: %v", err)
	}

	if len(data.ASNs) != 1 || data.ASNs[0].ASN != "212345" {
		t.Errorf("ASN list unexpected: %+v", data.ASNs)
	}

	if !data.Sessions[0].DefaultRoute {
		t.Error("DefaultRoute should be true (normalised from \"1\")")
	}

	if data.Sessions[0].Multihop {
		t.Error("Multihop should be false (normalised from \"0\")")
	}

	if err := c.BGP.SubmitASN(ctx, "AS212345"); err != nil {
		t.Fatalf("SubmitASN: %v", err)
	}

	if err := c.BGP.CreateSession(ctx, "1", client.CreateBGPSessionRequest{
		Redundant:    true,
		DefaultRoute: true,
		IPIDv4:       "7795",
		IPIDv6:       "7796",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := c.BGP.DeleteSession(ctx, "1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestBGPCreateSessionRequiresIP(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	err := c.BGP.CreateSession(context.Background(), "1", client.CreateBGPSessionRequest{})
	if err == nil {
		t.Fatal("expected error when neither IPIDv4 nor IPIDv6 is set")
	}
}
