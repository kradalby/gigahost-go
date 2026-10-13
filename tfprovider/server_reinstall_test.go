package tfprovider

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// Fixtures for the reinstall walk. Two Debian releases so an os change has
// somewhere to go.
const (
	distrosJSON = `{"meta":{"status":200},"data":[{"dist_id":"1","dist_name":"Debian"}]}`

	debianOSesJSON = `{"meta":{"status":200},"data":[` +
		`{"os_id":"88","os_name":"Debian 11 64-bit","os_release":"11"},` +
		`{"os_id":"101","os_name":"Debian 12 64-bit","os_release":"12"}]}`

	reinstallOKJSON = `{"success":true,"message":"Reinstall started","reboot":true,"root_passwd":"rotated-secret"}`

	// installingJSON is server 18394 mid-install of Debian 12, the only time
	// the API shows the new root password.
	installingJSON = `{"meta":{"status":200},"data":[{"srv_id":"18394","srv_status":true,` +
		`"srv_status_install":true,"os_id":"101","srv_primary_ip":"192.0.2.10","ips":[],` +
		`"install_details":{"root_password":"from-details","sshkey":false}}]}`
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

		// Only the hostname read before the reinstall succeeds, so the wait
		// runs out.
		wireReinstallRoutes(h, func(_ *http.Request, call int) (int, string) {
			if call == 1 {
				return http.StatusOK, serverJSON("18394", "88")
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

		// The hostname read and the first settle read succeed; every later
		// read fails.
		wireReinstallRoutes(h, func(_ *http.Request, call int) (int, string) {
			if call <= 2 {
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

// TestServerReinstallWithoutPasswordIsNull: the live API answers reinstall
// with an empty root_passwd. Storing "" reads as a known empty password;
// null says the API gave none.
func TestServerReinstallWithoutPasswordIsNull(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
		h.api.Route(http.MethodPost, "/servers/*").
			Respond(http.StatusOK, `{"success":true,"reboot":true,"root_passwd":""}`)
		h.api.Route(http.MethodGet, "/servers/*").Respond(http.StatusOK, serverJSON("18394", "101"))

		prior := managedServerState(objType, "debian-11")
		config := mkObject(objType, map[string]tftypes.Value{
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
		})

		planned := h.plan("gigahost_server", prior, config)
		res := h.apply("gigahost_server", prior, planned.plannedValue, config)

		if res.HasError() {
			t.Fatalf("apply: %s", res.ErrorText())
		}

		if pw := res.State["password"]; !pw.IsNull() {
			t.Errorf("password = %v, want null when the API returns none", pw)
		}
	})
}

// TestServerReinstallSendsSSHKeys: an os change re-authorizes the server's
// keys. Without ssh_keys on the reinstall the new OS has none and is
// unreachable.
func TestServerReinstallSendsSSHKeys(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		var sent string

		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
		h.api.Route(http.MethodPost, "/servers/*").RespondWith(func(r *http.Request, _ int) (int, string) {
			b, _ := io.ReadAll(r.Body)
			sent = string(b)

			return http.StatusOK, reinstallOKJSON
		})
		h.api.Route(http.MethodGet, "/servers/*").Respond(http.StatusOK, serverJSON("18394", "101"))

		prior := mkObject(objType, map[string]tftypes.Value{
			"id": tfStr("18394"), "order_id": tfStr("34147"), "platform": tfStr("cloud"),
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "region": tfStr("sfj"),
			"os": tfStr("debian-11"), "ssh_keys": tfStrList("2899"), "ip": tfStr("192.0.2.10"),
			"ips": emptyList(objType, "ips"),
		})
		config := mkObject(objType, map[string]tftypes.Value{
			"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
			"ssh_keys": tfStrList("2899"),
		})

		planned := h.plan("gigahost_server", prior, config)
		if res := h.apply("gigahost_server", prior, planned.plannedValue, config); res.HasError() {
			t.Fatalf("apply: %s", res.ErrorText())
		}

		if !strings.Contains(sent, `"ssh_keys":["2899"]`) {
			t.Errorf("reinstall body = %s, want ssh_keys [2899]", sent)
		}
	})
}

// TestServerReinstallCapturesInstallPassword: the reinstall response carries no
// password; the running install's record does. It is the only chance to read it.
func TestServerReinstallCapturesInstallPassword(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
		h.api.Route(http.MethodPost, "/servers/*").
			Respond(http.StatusOK, `{"success":true,"reboot":true,"root_passwd":""}`)
		h.api.Route(http.MethodGet, "/servers/*").RespondWith(func(_ *http.Request, call int) (int, string) {
			switch call {
			case 1: // hostname read before the reinstall
				return http.StatusOK, serverJSON("18394", "88")
			case 2:
				return http.StatusOK, installingJSON
			}

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

		if got := str(res.State, "password"); got != "from-details" {
			t.Errorf("password = %q, want the one install_details exposed", got)
		}
	})
}

// liveServerJSON is server 18394 shaped as the live API reports it: an empty
// srv_hostname, and the hostname it was deployed with in srv_name.
const liveServerJSON = `{"meta":{"status":200},"data":[{"srv_id":"18394",` +
	`"srv_label":"srv18394.gigahost.no","srv_name":"web01.example.com","srv_hostname":"",` +
	`"srv_status":true,"srv_status_install":false,"os_id":"88","srv_primary_ip":"192.0.2.10","ips":[]}]}`

// debian11To12 plans an os change on a managed server with no hostname
// configured, the common case. It returns prior, planned and config.
func debian11To12(h *harness) (tftypes.Value, tftypes.Value, tftypes.Value) {
	h.t.Helper()

	objType := h.resourceObjectType("gigahost_server")

	prior := managedServerState(objType, "debian-11")
	config := mkObject(objType, map[string]tftypes.Value{
		"type": tfStr("value"), "size": tfStr("2c-4gb-40gb"), "os": tfStr("debian-12"),
	})

	res := h.plan("gigahost_server", prior, config)
	if res.HasError() {
		h.t.Fatalf("plan: %s", res.ErrorText())
	}

	return prior, res.plannedValue, config
}

// TestServerReinstallKeepsDeployHostname: with no hostname configured the
// reinstall must still send the server's own one. An empty hostname makes the
// API name the new OS after its IP.
func TestServerReinstallKeepsDeployHostname(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)

		var sent string

		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
		h.api.Route(http.MethodPost, "/servers/*").RespondWith(func(r *http.Request, _ int) (int, string) {
			b, _ := io.ReadAll(r.Body)
			sent = string(b)

			return http.StatusOK, reinstallOKJSON
		})
		h.api.Route(http.MethodGet, "/servers/*").RespondWith(func(_ *http.Request, call int) (int, string) {
			if call == 1 {
				return http.StatusOK, liveServerJSON
			}

			return http.StatusOK, serverJSON("18394", "101")
		})

		prior, planned, config := debian11To12(h)
		if res := h.apply("gigahost_server", prior, planned, config); res.HasError() {
			t.Fatalf("apply: %s", res.ErrorText())
		}

		if !strings.Contains(sent, `"hostname":"web01.example.com"`) {
			t.Errorf("reinstall body = %s, want the deploy hostname from srv_name", sent)
		}
	})
}

// silentReinstall stands in for the live reinstall endpoint, which starts the
// install and then holds the request past the client deadline.
func silentReinstall(_ *http.Request, _ int) (int, string) {
	time.Sleep(2 * gigahost.DefaultHTTPTimeout)

	return http.StatusOK, reinstallOKJSON
}

// TestServerReinstallUnansweredButStarted: the request times out, yet the
// server reports the requested OS installing. The disk is already being
// wiped, so failing would leave state on the old OS and lose the new
// password; the apply proceeds, and says why.
func TestServerReinstallUnansweredButStarted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)

		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
		h.api.Route(http.MethodPost, "/servers/*").RespondWith(silentReinstall)
		h.api.Route(http.MethodGet, "/servers/*").RespondWith(func(_ *http.Request, call int) (int, string) {
			switch call {
			case 1:
				return http.StatusOK, liveServerJSON
			case 2, 3:
				return http.StatusOK, installingJSON
			}

			return http.StatusOK, serverJSON("18394", "101")
		})

		prior, planned, config := debian11To12(h)

		res := h.apply("gigahost_server", prior, planned, config)
		if res.HasError() {
			t.Fatalf("apply: %s", res.ErrorText())
		}

		if !strings.Contains(res.WarningText(), "without an acknowledgement") {
			t.Errorf("warning %q does not say the request went unanswered", res.WarningText())
		}

		if got := str(res.State, "os"); got != "debian-12" {
			t.Errorf("os = %q, want debian-12", got)
		}

		if got := str(res.State, "password"); got != "from-details" {
			t.Errorf("password = %q, want the one install_details exposed", got)
		}
	})
}

// TestServerReinstallFailureNotConfirmed: only a server installing the
// requested OS turns a failed request into progress. A timeout with nothing
// installing, or an outright refusal, must fail the apply.
func TestServerReinstallFailureNotConfirmed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		post      func(*http.Request, int) (int, string)
		wantReads int // GET /servers/{id} calls, including the hostname read
		wantErr   string
	}{
		{
			name:      "unanswered, nothing installing",
			post:      silentReinstall,
			wantReads: 2,
			wantErr:   "the install has not started",
		},
		{
			// A refusal is final: no read-back, even with an install running.
			name: "refused",
			post: func(*http.Request, int) (int, string) {
				return http.StatusBadRequest,
					`{"meta":{"status":400,"status_message":"400 Bad Request","message":"Invalid data received."}}`
			},
			wantReads: 1,
			wantErr:   "Invalid data received.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t)

				h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
				h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)
				h.api.Route(http.MethodPost, "/servers/*").RespondWith(tc.post)

				// After the hostname read, the refused case sees an install
				// running; the read-back must still never happen.
				reads := h.api.Route(http.MethodGet, "/servers/*").
					RespondWith(func(_ *http.Request, call int) (int, string) {
						if call == 1 || tc.wantReads > 1 {
							return http.StatusOK, liveServerJSON
						}

						return http.StatusOK, installingJSON
					})

				prior, planned, config := debian11To12(h)

				res := h.apply("gigahost_server", prior, planned, config)
				if !res.HasError() {
					t.Fatal("apply succeeded without proof the reinstall started")
				}

				if !strings.Contains(res.ErrorText(), tc.wantErr) {
					t.Errorf("error %q, want it to contain %q", res.ErrorText(), tc.wantErr)
				}

				if got := reads.Calls(); got != tc.wantReads {
					t.Errorf("server read %d times, want %d", got, tc.wantReads)
				}
			})
		})
	}
}
