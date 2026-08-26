package tfprovider

import (
	"net/http"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// catalogFixture is the real live payload shape, reused from the client tests.
func catalogFixture(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile("../client/testdata/deploy/catalog.json")
	if err != nil {
		t.Fatalf("read catalog fixture: %v", err)
	}

	return string(b)
}

// wireImportRoutes answers what Read and adoptImported ask for: the server
// record, the catalog, and the OS list. The fixture's KVM Value VPS 4GB is
// 2 cores / 4 GB, so a server reporting the same passes adoption's hardware
// verification.
func wireImportRoutes(t *testing.T, h *harness) {
	t.Helper()

	h.api.Route(http.MethodGet, "/servers/*").
		Respond(http.StatusOK, serverJSON("18394", "101"))
	h.api.Route(http.MethodGet, "/deploy/servers").
		Respond(http.StatusOK, catalogFixture(t))
	h.api.Route(http.MethodGet, "/reinstall/distro").
		Respond(http.StatusOK, distrosJSON)
	h.api.Route(http.MethodGet, "/reinstall/distro/*").
		Respond(http.StatusOK, debianOSesJSON)
}

// TestServerImportThenApplyConverges is the regression guard for import, the
// documented onboarding path for an existing server.
//
// After `terraform import` the product selectors are null in state, which is
// what routes the first apply through adoptImported. That branch writes the
// catalog facts — memory_gb, storage_gb, rate_hourly, rate_monthly — into
// state. A Computed attribute that is null in prior state plans as null, not
// unknown, so unless ModifyPlan marks these unknown the apply fails with
// "provider produced inconsistent result after apply: was null, but now 4"
// and import can never converge.
//
// The harness asserts that contract on every apply, so this test fails the
// moment the adoption branch stops marking them.
func TestServerImportThenApplyConverges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")
		wireImportRoutes(t, h)

		// 1. Import sets only the id.
		imported := h.importState("gigahost_server", "18394")
		if len(imported) != 1 {
			t.Fatalf("import produced %d objects, want 1", len(imported))
		}

		if got := str(imported[0], "id"); got != "18394" {
			t.Fatalf("imported id = %q, want 18394", got)
		}

		// 2. Read fills in what the live record can supply. The product selectors
		//    stay null — the API reports product_id "0" for live servers.
		prior := mkObject(objType, map[string]tftypes.Value{"id": tfStr("18394")})

		refreshed := h.read("gigahost_server", prior)
		if refreshed == nil {
			t.Fatal("Read dropped a server that exists")
		}

		for _, attr := range []string{"type", "size"} {
			if v := str(refreshed, attr); v != "" {
				t.Errorf("%q = %q after import; it must stay null so the first apply "+
					"takes the adoption branch", attr, v)
			}
		}

		// 3. The practitioner's config names the machine they imported.
		config := mkObject(objType, map[string]tftypes.Value{
			"type":     tfStr("value"),
			"size":     tfStr("2c-4gb-40gb"),
			"os":       tfStr("debian-12"),
			"backups":  tfBool(false),
			"ssh_keys": tfStrList(),
		})

		priorState := mkObject(objType, refreshed)

		planned := h.plan("gigahost_server", priorState, config)
		if planned.HasError() {
			t.Fatalf("plan after import: %s", planned.ErrorText())
		}

		// Adoption must not classify the imported os as a reinstall.
		if planned.Replaces("os") {
			t.Error("adopting an imported server planned a replacement; it would cancel a live machine")
		}

		// 4. The apply. The harness checks the framework contract, so a regression
		//    in the adoption branch surfaces here as a named attribute.
		res := h.apply("gigahost_server", priorState, planned.plannedValue, config)
		if res.HasError() {
			t.Fatalf("first apply after import: %s", res.ErrorText())
		}

		for _, attr := range []string{"type", "size"} {
			if str(res.State, attr) == "" {
				t.Errorf("%q is still null after adoption; the config values must be "+
					"written into state", attr)
			}
		}
	})
}

// TestServerImportRefusesMismatchedSize proves adoption verifies the hardware
// before writing config into state. Adopting a machine that does not match
// what the config claims would let a later change replace — that is, cancel —
// the wrong server.
func TestServerImportRefusesMismatchedSize(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")
		wireImportRoutes(t, h)

		prior := mkObject(objType, map[string]tftypes.Value{
			"id":     tfStr("18394"),
			"os":     tfStr("debian-12"),
			"cores":  tfNum(2),
			"region": tfStr("sfj"),
			"ips":    emptyList(objType, "ips"),
		})

		// The live server is 2 cores / 4 GB; claim an 8 GB size instead.
		config := mkObject(objType, map[string]tftypes.Value{
			"type":     tfStr("value"),
			"size":     tfStr("4c-8gb-80gb"),
			"os":       tfStr("debian-12"),
			"ssh_keys": tfStrList(),
		})

		planned := h.plan("gigahost_server", prior, config)
		if planned.HasError() {
			t.Fatalf("plan: %s", planned.ErrorText())
		}

		res := h.apply("gigahost_server", prior, planned.plannedValue, config)
		if !res.HasError() {
			t.Fatal("adopting a server whose hardware does not match the configured size " +
				"must fail; silently adopting it risks cancelling the wrong machine")
		}
	})
}

// TestServerReadDropsCancelledServer covers the other end of Read: a server
// cancelled out of band leaves state, so the next plan recreates it rather
// than erroring forever.
func TestServerReadDropsCancelledServer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		h.api.Route(http.MethodGet, "/servers/*").
			Respond(http.StatusNotFound,
				`{"meta":{"status":404,"status_message":"404 Not Found","message":"Server not found."}}`)

		prior := mkObject(objType, map[string]tftypes.Value{"id": tfStr("18394")})

		start := time.Now()

		if got := h.read("gigahost_server", prior); got != nil {
			t.Errorf("Read kept a server that is gone upstream: id=%q", str(got, "id"))
		}

		// Every read 404s, so Read spends the full absence budget before it
		// concludes the server is really gone: serverAbsenceReads reads with a
		// delay between each pair.
		assertElapsed(t, start, (serverAbsenceReads-1)*serverAbsenceDelay)
	})
}

// TestServerReadToleratesTransientAbsence is the B14 guard: /servers omits
// live servers for tens of seconds after a change. A single miss must not be
// read as "cancelled", or Terraform destroys and recreates a healthy machine.
func TestServerReadToleratesTransientAbsence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		objType := h.resourceObjectType("gigahost_server")

		h.api.Route(http.MethodGet, "/servers/*").
			RespondWith(func(_ *http.Request, call int) (int, string) {
				if call <= 2 {
					return http.StatusNotFound,
						`{"meta":{"status":404,"status_message":"404 Not Found","message":"Server not found."}}`
				}

				return http.StatusOK, serverJSON("18394", "101")
			})
		h.api.Route(http.MethodGet, "/deploy/servers").Respond(http.StatusOK, catalogFixture(t))
		h.api.Route(http.MethodGet, "/reinstall/distro").Respond(http.StatusOK, distrosJSON)
		h.api.Route(http.MethodGet, "/reinstall/distro/*").Respond(http.StatusOK, debianOSesJSON)

		prior := mkObject(objType, map[string]tftypes.Value{"id": tfStr("18394")})

		start := time.Now()

		got := h.read("gigahost_server", prior)

		// The server reappears on the third read, so Read waits out two
		// absence delays and no more — it must not burn the whole budget once
		// the server is back.
		assertElapsed(t, start, 2*serverAbsenceDelay)

		if got == nil {
			t.Fatal("a server that reappears on the third read was reported as cancelled; " +
				"Terraform would recreate a live machine (upstream B14)")
		}

		if id := str(got, "id"); id != "18394" {
			t.Errorf("id = %q, want 18394", id)
		}
	})
}
