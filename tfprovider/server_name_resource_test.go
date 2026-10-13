package tfprovider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// renamedServerJSON is the live shape after PUT /servers/{id}/name: srv_name
// moves, srv_label keeps the Gigahost-assigned value, srv_hostname is empty.
const renamedServerJSON = `{"meta":{"status":200},"data":[{"srv_id":"18902",` +
	`"srv_name":"tf-renamed","srv_label":"srv18902.gigahost.no","srv_hostname":""}]}`

func serverNameState(objType tftypes.Object, name string) tftypes.Value {
	return mkObject(objType, map[string]tftypes.Value{
		"id": tfStr("18902"), "server_id": tfStr("18902"), "name": tfStr(name),
	})
}

// TestServerNameReadsSrvName pins the field the name endpoint changes. Read
// (and so import) reporting srv_label shows a value the API never changes.
func TestServerNameReadsSrvName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_server_name")

	h.api.Expect(http.MethodGet, "/servers/18902").Respond(http.StatusOK, renamedServerJSON)

	got := h.read("gigahost_server_name", serverNameState(objType, "stale"))
	if name := str(got, "name"); name != "tf-renamed" {
		t.Errorf("name = %q, want srv_name %q", name, "tf-renamed")
	}
}

// TestServerNameDestroyResetsToLabel pins a destroy the API accepts: it
// rejects an empty name, and srv_hostname is empty on live servers.
func TestServerNameDestroyResetsToLabel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_server_name")

	h.api.Expect(http.MethodGet, "/servers/18902").Respond(http.StatusOK, renamedServerJSON)
	h.api.Expect(http.MethodPut, "/servers/18902/name").WithJSON(`{"name":"srv18902.gigahost.no"}`)

	res := h.apply("gigahost_server_name", serverNameState(objType, "tf-renamed"), nullObject(objType), nullObject(objType))
	if res.HasError() {
		t.Fatalf("destroy: %s", res.ErrorText())
	}
}
