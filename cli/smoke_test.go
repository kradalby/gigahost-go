//go:build e2e

package cli_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/kradalby/gigahost-go/cli"
)

// Live CLI smoke tests: run read-only commands in-process against the
// real API and assert they exit 0 with valid JSON output. This catches
// wiring rot between the command tree and the client that the
// structural TestCommandTree cannot (flag plumbing, render paths,
// decode of live payloads through CLI eyes).
//
// Gated like the e2e package: the `e2e` build tag keeps a plain
// `go test ./...` offline, and the suite skips without GIGAHOST_TOKEN.

// smokeToken skips the test unless GIGAHOST_TOKEN is set.
func smokeToken(t *testing.T) {
	t.Helper()

	if os.Getenv("GIGAHOST_TOKEN") == "" {
		t.Skip("GIGAHOST_TOKEN not set; skipping live CLI smoke test")
	}
}

// runCLI executes the command tree in-process with JSON output.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	var out, errOut bytes.Buffer

	code := cli.Run(context.Background(), append([]string{"-o", "json"}, args...), cli.Options{
		Version: "smoke",
		Commit:  "smoke",
		Stdout:  &out,
		Stderr:  &errOut,
	})

	return code, out.String(), errOut.String()
}

// assertSmoke runs the command and requires exit 0 + valid JSON (or
// empty) output. A 403 from the API skips: the standard test token
// lacks some scopes (see docs/upstream-issues.md A1-A3) and that is a
// token limitation, not a CLI defect.
func assertSmoke(t *testing.T, args ...string) {
	t.Helper()
	smokeToken(t)

	code, out, errOut := runCLI(t, args...)

	if code != 0 {
		if strings.Contains(errOut, "403") {
			t.Skipf("token lacks permission for %q: %s", strings.Join(args, " "), strings.TrimSpace(errOut))
		}

		t.Fatalf("%q exited %d: %s", strings.Join(args, " "), code, errOut)
	}

	if trimmed := strings.TrimSpace(out); trimmed != "" {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
			t.Fatalf("%q output is not valid JSON: %v\n%s", strings.Join(args, " "), err, trimmed)
		}
	}
}

func TestSmokeAccountShow(t *testing.T)    { assertSmoke(t, "account", "show") }
func TestSmokeAccountBalance(t *testing.T) { assertSmoke(t, "account", "balance") }
func TestSmokeAccountInvoices(t *testing.T) {
	assertSmoke(t, "account", "invoices")
}

func TestSmokeAccountSSHKeysList(t *testing.T) {
	assertSmoke(t, "account", "ssh-keys", "list")
}

func TestSmokeAccountAPIKeysList(t *testing.T) {
	assertSmoke(t, "account", "api-keys", "list") // skips on 403 (A1)
}

func TestSmokeAccountUsersList(t *testing.T) {
	assertSmoke(t, "account", "users", "list")
}

func TestSmokeDeployCatalog(t *testing.T) { assertSmoke(t, "deploy", "catalog") }
func TestSmokeDeployTypes(t *testing.T)   { assertSmoke(t, "deploy", "types") }
func TestSmokeDeploySizes(t *testing.T)   { assertSmoke(t, "deploy", "sizes") }
func TestSmokeDeploySizesFiltered(t *testing.T) {
	assertSmoke(t, "deploy", "sizes", "--type", "value")
}
func TestSmokeDeployRegions(t *testing.T) { assertSmoke(t, "deploy", "regions") }
func TestSmokeDeployOS(t *testing.T)      { assertSmoke(t, "deploy", "os") }
func TestSmokeDeployOSFiltered(t *testing.T) {
	assertSmoke(t, "deploy", "os", "debian")
}
func TestSmokeDeployISOs(t *testing.T) { assertSmoke(t, "deploy", "isos") }

func TestSmokeServersList(t *testing.T) { assertSmoke(t, "servers", "list") }

func TestSmokeServersReinstallDistros(t *testing.T) {
	assertSmoke(t, "servers", "reinstall", "distros")
}

func TestSmokeServersReinstallOS(t *testing.T) {
	assertSmoke(t, "servers", "reinstall", "os", "2") // 2 = Debian
}

func TestSmokeDNSZonesList(t *testing.T) { assertSmoke(t, "dns", "zones", "list") }

func TestSmokeDNSDomainCheck(t *testing.T) {
	assertSmoke(t, "dns", "domain", "check", "tf-acc-smoke-unregistered.no")
}

func TestSmokeBGPShow(t *testing.T) { assertSmoke(t, "bgp", "show") }
