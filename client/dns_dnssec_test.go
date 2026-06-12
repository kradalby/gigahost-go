package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

// External-DS and PTR payloads are synthesized from the API docs; the
// test account has no externally hosted registered domain or IP prefix,
// so no captured live fixtures exist for these endpoints.

func TestDNSGetExternalDSRecords(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/zones/7777/ds-records/external").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"ds_records":[{"keyTag":12345,"alg":13,"digestType":2,"digest":"49FD46E6C4B45C55D4AC69CBD3CD34AC1AFE51DE"}]}}`)

	res, err := c.DNS.GetExternalDSRecords(context.Background(), "7777")
	if err != nil {
		t.Fatalf("GetExternalDSRecords: %v", err)
	}

	if len(res.DSRecords) != 1 || res.DSRecords[0].KeyTag != 12345 || res.DSRecords[0].Algorithm != 13 {
		t.Errorf("DSRecordsExternal = %+v", res)
	}

	if _, err := c.DNS.GetExternalDSRecords(context.Background(), ""); err == nil {
		t.Error("expected error for empty zoneID")
	}
}

func TestDNSSubmitExternalDSRecords(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/dns/zones/7777/ds-records/external").
		WithJSON(`{"ds_records":[{"keyTag":12345,"alg":13,"digestType":2,"digest":"49FD46E6C4B45C55D4AC69CBD3CD34AC1AFE51DE"}]}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.DNS.SubmitExternalDSRecords(context.Background(), "7777", []client.DSRecord{
		{KeyTag: 12345, Algorithm: 13, DigestType: 2, Digest: "49FD46E6C4B45C55D4AC69CBD3CD34AC1AFE51DE"},
	})
	if err != nil {
		t.Fatalf("SubmitExternalDSRecords: %v", err)
	}

	if err := c.DNS.SubmitExternalDSRecords(context.Background(), "", []client.DSRecord{{}}); err == nil {
		t.Error("expected error for empty zoneID")
	}

	if err := c.DNS.SubmitExternalDSRecords(context.Background(), "7777", nil); err == nil {
		t.Error("expected error for empty records")
	}
}

func TestDNSCreatePTRZone(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/dns/zones/ptr").
		WithJSON(`{"prefix":"185.125.168.0/24","ip_version":"ipv4","zone_name":"168.125.185.in-addr.arpa"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"zone_id":"8888"}}`)

	res, err := c.DNS.CreatePTRZone(context.Background(), client.CreatePTRZoneRequest{
		Prefix:    "185.125.168.0/24",
		IPVersion: client.IPVersionIPv4,
		ZoneName:  "168.125.185.in-addr.arpa",
	})
	if err != nil {
		t.Fatalf("CreatePTRZone: %v", err)
	}

	if res.ZoneID != "8888" {
		t.Errorf("ZoneID = %q", res.ZoneID)
	}

	if _, err := c.DNS.CreatePTRZone(context.Background(), client.CreatePTRZoneRequest{}); err == nil {
		t.Error("expected error for missing required fields")
	}
}
