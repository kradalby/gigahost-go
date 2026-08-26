package tfprovider

import (
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Fixtures for the reinstall walk. Two Debian releases so an os change has
// somewhere to go.
const (
	distrosJSON = `{"meta":{"status":200},"data":[{"dist_id":"1","dist_name":"Debian"}]}`

	debianOSesJSON = `{"meta":{"status":200},"data":[` +
		`{"os_id":"88","os_name":"Debian 11 64-bit","os_release":"11"},` +
		`{"os_id":"101","os_name":"Debian 12 64-bit","os_release":"12"}]}`

	reinstallOKJSON = `{"success":true,"message":"Reinstall started","reboot":true,"root_passwd":"rotated-secret"}`
)

// serverJSON renders one /servers/{id} record. The endpoint answers with a
// single-element array, not an object.
func serverJSON(id, osID string) string {
	return `{"meta":{"status":200},"data":[{` +
		`"srv_id":"` + id + `","srv_label":"web01","srv_hostname":"web01.example.no",` +
		`"srv_status":true,"srv_status_install":false,"srv_status_rescue":false,` +
		`"srv_cores":2,"srv_ram":4,"os_id":"` + osID + `",` +
		`"srv_primary_ip":"192.0.2.10","ips":[]}]}`
}

// wireReinstallRoutes answers everything the os-change Update path asks for.
// serverGet decides what /servers/{id} does, so a test can make the
// post-reinstall read fail.
func wireReinstallRoutes(h *harness, serverGet func(r *http.Request, call int) (int, string)) {
	h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
	h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
	h.api.Route(http.MethodPost, "/servers/*").Respond(http.StatusOK, reinstallOKJSON)
	h.api.Route(http.MethodGet, "/servers/*").RespondWith(serverGet)
}

// managedServerState is a server already under management: type and size are
// present, which is what tells the provider this is not a fresh import.
func managedServerState(objType tftypes.Object, osSlug string) tftypes.Value {
	return mkObject(objType, map[string]tftypes.Value{
		"id":         tfStr("18394"),
		"order_id":   tfStr("34147"),
		"platform":   tfStr("cloud"),
		"type":       tfStr("value"),
		"size":       tfStr("2c-4gb-40gb"),
		"region":     tfStr("sfj"),
		"os":         tfStr(osSlug),
		"status":     tfStr("running"),
		"os_name":    tfStr("Debian 11 64-bit"),
		"os_release": tfStr("11"),
		"password":   tfStr("old-secret"),
		"ip":         tfStr("192.0.2.10"),
		"cores":      tfNum(2),
		"memory_gb":  tfNum(4),
		"ips":        emptyList(objType, "ips"),
	})
}

// TestServerReinstallPlanBlanksRotatedAttributes pins the full set of
// attributes an in-place OS reinstall rotates. Any one left known keeps its
// pre-install value and the apply fails with "inconsistent result after
// apply" — which is how os_name and os_release were found missing.
func TestServerReinstallPlanBlanksRotatedAttributes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	objType := h.resourceObjectType("gigahost_server")

	prior := managedServerState(objType, "debian-11")
	config := mkObject(objType, map[string]tftypes.Value{
		"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
	})

	res := h.plan("gigahost_server", prior, config)
	if res.HasError() {
		t.Fatalf("plan: %s", res.ErrorText())
	}

	for _, attr := range []string{"password", "status", "os_name", "os_release"} {
		if !res.Unknown(attr) {
			t.Errorf("%q is not unknown in the plan; a reinstall rotates it, so the "+
				"apply will fail with \"inconsistent result after apply\"", attr)
		}
	}

	if res.Replaces("os") {
		t.Error("an os-to-os change must reinstall in place, not replace the server")
	}

	if res.WarningText() == "" {
		t.Error("an in-place reinstall wipes the disk; it must warn")
	}
}

// TestServerReinstallKeepsPasswordWhenWaitFails covers the worst moment in
// this resource: the reinstall has happened — disk wiped, root password
// rotated — and then the settle wait times out because /servers keeps
// failing. The password is returned exactly once. If the error return
// discards it, state holds a password that no longer opens the machine and
// there is no way to recover it.
func TestServerReinstallKeepsPasswordWhenWaitFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		// The read never recovers, so the wait runs out.
		wireReinstallRoutes(h, func(_ *http.Request, _ int) (int, string) {
			return http.StatusInternalServerError,
				`{"meta":{"status":500,"status_message":"500 Internal Server Error","message":"upstream blip"}}`
		})

		prior := managedServerState(objType, "debian-11")
		config := mkObject(objType, map[string]tftypes.Value{
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
		})

		planned := h.plan("gigahost_server", prior, config)
		if planned.HasError() {
			t.Fatalf("plan: %s", planned.ErrorText())
		}

		res := h.apply("gigahost_server", prior, planned.plannedValue, config)

		// Erroring is correct — the reinstall really did not settle.
		if !res.HasError() {
			t.Fatal("a reinstall that never settles must report an error")
		}

		if got := str(res.State, "password"); got != "rotated-secret" {
			t.Errorf("password = %q after a failed settle, want the rotated one; "+
				"it is returned once and is unrecoverable if state drops it", got)
		}

		// The cause has to survive too, or the user sees only a bare timeout.
		if !strings.Contains(res.ErrorText(), "upstream blip") {
			t.Errorf("error %q loses the underlying cause of the failed poll", res.ErrorText())
		}
	})
}

// TestServerReinstallSurvivesFailedPostRead is the branch this change set
// introduced. The reinstall settles, then the read that refreshes runtime
// facts fails — upstream B14 documents that read as flaky in exactly that
// window. ModifyPlan marked four attributes unknown, and Terraform rejects a
// state that still carries one, reporting it to the user as a provider bug
// immediately after their disk was wiped. Every one must be resolved.
func TestServerReinstallSurvivesFailedPostRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		// First read settles the install; every later read fails.
		wireReinstallRoutes(h, func(_ *http.Request, call int) (int, string) {
			if call == 1 {
				return http.StatusOK, serverJSON("18394", "101")
			}

			return http.StatusInternalServerError,
				`{"meta":{"status":500,"status_message":"500 Internal Server Error","message":"upstream blip"}}`
		})

		prior := managedServerState(objType, "debian-11")
		config := mkObject(objType, map[string]tftypes.Value{
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
		})

		planned := h.plan("gigahost_server", prior, config)
		if planned.HasError() {
			t.Fatalf("plan: %s", planned.ErrorText())
		}

		// apply() asserts the framework contract: no unknown may reach state.
		res := h.apply("gigahost_server", prior, planned.plannedValue, config)

		if res.HasError() {
			t.Fatalf("apply failed instead of recovering from a flaky read: %s", res.ErrorText())
		}

		if res.WarningText() == "" {
			t.Error("the read failed after a destructive reinstall; that must be surfaced, not swallowed")
		}

		if got := str(res.State, "password"); got != "rotated-secret" {
			t.Errorf("password = %q, want the rotated one", got)
		}
	})
}

// TestServerReinstallRecordsRotatedPassword covers the other half: on the
// happy path the new root password must reach state, because it is returned
// exactly once and is unrecoverable afterwards.
func TestServerReinstallRecordsRotatedPassword(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		wireReinstallRoutes(h, func(_ *http.Request, _ int) (int, string) {
			return http.StatusOK, serverJSON("18394", "101")
		})

		prior := managedServerState(objType, "debian-11")
		config := mkObject(objType, map[string]tftypes.Value{
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
		})

		planned := h.plan("gigahost_server", prior, config)
		res := h.apply("gigahost_server", prior, planned.plannedValue, config)

		if res.HasError() {
			t.Fatalf("apply: %s", res.ErrorText())
		}

		if got := str(res.State, "password"); got != "rotated-secret" {
			t.Errorf("password = %q, want the rotated one; it is returned once and "+
				"cannot be recovered", got)
		}

		if got := str(res.State, "status"); got != "running" {
			t.Errorf("status = %q, want %q from the server record", got, "running")
		}
	})
}
