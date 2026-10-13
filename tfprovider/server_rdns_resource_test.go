package tfprovider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The live record lists the primary IPv6 entry first under the same ip_id as
// its IPv4 address; Read must refresh from the IPv4 entry or every apply
// leaves a diff.
func TestServerRDNSReadUsesIPv4Entry(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.Route(http.MethodGet, "/servers/18394").Respond(http.StatusOK, `{"meta":{"status":200},"data":[{
		"srv_id":"18394","ips":[
			{"ip_id":"7797","sub_id":"4038","ip_v4v6":"ipv6","ip_address":"2001:db8::236","ip_reverse":"","ip_reverse_v6":""},
			{"ip_id":"7797","sub_id":"4038","ip_v4v6":"ipv4","ip_address":"192.0.2.236","ip_reverse":"mail.example.no","ip_reverse_v6":""}
		]}]}`)

	objType := h.resourceObjectType("gigahost_server_rdns")
	got := h.read("gigahost_server_rdns", mkObject(objType, map[string]tftypes.Value{
		"id": tfStr("18394/7797"), "server_id": tfStr("18394"), "ip_id": tfStr("7797"), "dns": tfStr("mail.example.no"),
	}))

	if dns := str(got, "dns"); dns != "mail.example.no" {
		t.Fatalf("dns = %q, want the IPv4 entry's reverse", dns)
	}
}
