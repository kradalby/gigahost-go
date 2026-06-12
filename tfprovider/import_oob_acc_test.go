package tfprovider_test

// Out-of-band (OOB) import acceptance suite.
//
// Each test proves the full ownership-transfer story for one importable
// resource: create the object with the gigahost CLI (in-process, so no binary
// is required), import it into Terraform via `terraform import`, assert the
// imported attributes, then apply a matching config that must converge to an
// empty plan, and finally let the framework destroy the (CLI-created) object.
// CheckDestroy then proves the destroy actually reached the live API — i.e.
// Terraform now fully owns the resource the CLI created.
//
// Design notes (why these steps and not ImportStateVerify):
//   - There is no prior apply step, so there is no "prior state" to verify the
//     import against; ImportStateVerify is therefore unusable. Instead we use
//     ImportStateCheck to assert the imported attributes, and a follow-up
//     Config step whose post-apply empty plan proves convergence.
//   - ImportStatePersist: true makes the imported state the real, persisted
//     state, so the follow-up Config step and the final destroy run against it.
//     That is what lets CheckDestroy observe the CLI-created object being
//     destroyed by Terraform.
//
// Run the cheap core (token + a zone apex) like this:
//
//	TF_ACC=1 GIGAHOST_TOKEN=… GIGAHOST_TEST_ZONE_APEX=… \
//	  go test ./tfprovider/ -run TestAccImportOOB -v -count=1
//
// Env-var matrix (a test skips cleanly when its vars are unset):
//
//	Test                            | Required env vars
//	--------------------------------|--------------------------------------------
//	DNSZone / DNSZoneByName /        | GIGAHOST_TOKEN, GIGAHOST_TEST_ZONE_APEX
//	  DNSZoneImportBlock             |
//	DNSRecord / DNSRedirect          | GIGAHOST_TOKEN, GIGAHOST_TEST_ZONE_APEX
//	AccountSSHKey                    | GIGAHOST_TOKEN
//	AccountAPIKey                    | GIGAHOST_TOKEN (skips on 403 / no list scope)
//	DNSSEC                           | GIGAHOST_TOKEN, GIGAHOST_TEST_REGISTERED_ZONE
//	DNSNameservers                   | GIGAHOST_TOKEN, GIGAHOST_TEST_REGISTERED_ZONE,
//	                                 |   GIGAHOST_TEST_NS1, GIGAHOST_TEST_NS2
//	DNSExternalDS                    | GIGAHOST_TOKEN, GIGAHOST_TEST_EXTERNAL_ZONE,
//	                                 |   GIGAHOST_TEST_DS_KEY_TAG, GIGAHOST_TEST_DS_DIGEST
//	DNSPTRZone / DNSPTRZoneByName    | GIGAHOST_TOKEN, GIGAHOST_TEST_IP_PREFIX,
//	                                 |   GIGAHOST_TEST_PTR_ZONE_NAME
//	ServerName                       | GIGAHOST_TOKEN, GIGAHOST_TEST_SERVER_ID
//	ServerRDNS                       | GIGAHOST_TOKEN, GIGAHOST_TEST_SERVER_ID,
//	                                 |   GIGAHOST_TEST_SERVER_IP_ID
//	ServerSnapshot                   | GIGAHOST_TOKEN, GIGAHOST_TEST_SERVER_ID
//	BGPASN / BGPASNPrefixed          | GIGAHOST_TOKEN, GIGAHOST_TEST_ASN
//	BGPSession                       | GIGAHOST_TOKEN, GIGAHOST_TEST_ASN_ID,
//	                                 |   GIGAHOST_TEST_IPV4_IP_ID
//	Server                           | GIGAHOST_TOKEN, GIGAHOST_TEST_OOB_DEPLOY=1
//	                                 |   (REAL MONEY: deploys and cancels a server)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/kradalby/gigahost-go/cli"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// Shared gating + import-state assertion helpers
// ---------------------------------------------------------------------------

// requireToken skips (does not fail) the OOB test when GIGAHOST_TOKEN is unset.
//
// The OOB suite is run standalone (its own -run filter), so a missing token is
// "not configured for this run" rather than a hard misconfiguration — it must
// SKIP, like every other gate here, so `go test -run TestAccImportOOB` is green
// on a machine without credentials. (The shared testAccPreCheck fatals instead,
// which is right for tests gated behind a PreCheck callback meant to run.) This
// fires before any CLI call, so the live API is never touched when ungated.
func requireToken(t *testing.T) {
	t.Helper()

	if os.Getenv("GIGAHOST_TOKEN") == "" {
		t.Skip("GIGAHOST_TOKEN must be set for OOB import acceptance tests")
	}
}

// checkImportAttr asserts that the single imported resource has attr == want.
// ImportStateCheck receives the imported resources as a slice (import may, in
// pathological cases, yield several); the OOB resources always import exactly
// one, so we assert that and read from it.
func checkImportAttr(attr, want string) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("expected exactly one imported resource, got %d", len(states))
		}

		got := states[0].Attributes[attr]
		if got != want {
			return fmt.Errorf("imported attribute %q = %q, want %q", attr, got, want)
		}

		return nil
	}
}

// composeImportStateChecks runs several ImportStateCheckFuncs in order.
func composeImportStateChecks(checks ...resource.ImportStateCheckFunc) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		for _, c := range checks {
			if err := c(states); err != nil {
				return err
			}
		}

		return nil
	}
}

// oobBootstrapStep returns a plan-only TestStep that initialises the Terraform
// working directory before an ImportStatePersist step.
//
// Background: terraform-plugin-testing skips Init for ImportStatePersist steps
// (it assumes a prior Config step already ran it — see
// testing_new_import_state.go). More specifically, `terraform import` enforces
// the dependency lock file, and the provider address in the config must match
// the reattach key set by the framework (registry.opentofu.org/hashicorp/…).
// Without a preceding Config step the working dir has never seen the merged
// config (which carries the required_providers source), so `import` resolves
// the provider to the default registry.terraform.io address — which does not
// match the reattach key — and fails with "Inconsistent dependency lock file".
// A plan-only step runs the merged config once so the working dir is correctly
// primed; the would-be create is expected (hence ExpectNonEmptyPlan) and then
// discarded.
func oobBootstrapStep(config string) resource.TestStep {
	return resource.TestStep{
		Config:             config,
		PlanOnly:           true,
		ExpectNonEmptyPlan: true,
	}
}

// testAccOOBProviderConfig returns a provider block that explicitly declares
// the gigahost source address, matching the TF_REATTACH_PROVIDERS key the
// framework uses.  Without the source, `provider "gigahost" {}` resolves to
// the default registry.terraform.io address, which differs from the reattach
// key (registry.opentofu.org when TF_ACC_PROVIDER_HOST is set by TestMain),
// causing `terraform import` to fail with an "Inconsistent dependency lock
// file" error even though the provider is running in-process.
//
// This is only needed for OOB import tests, which use the raw config in
// testStepNewImportState without the framework's mergedConfig transform.
// Regular Config steps go through mergedConfig which injects the source
// automatically.
func testAccOOBProviderConfig() string {
	host := os.Getenv("TF_ACC_PROVIDER_HOST")
	if host == "" {
		host = "registry.terraform.io"
	}

	ns := os.Getenv("TF_ACC_PROVIDER_NAMESPACE")
	if ns == "" {
		ns = "hashicorp"
	}

	return fmt.Sprintf(`
terraform {
  required_providers {
    gigahost = {
      source = %q
    }
  }
}

provider "gigahost" {}
`, host+"/"+ns+"/gigahost")
}

// ---------------------------------------------------------------------------
// Task 23: harness helper
// ---------------------------------------------------------------------------

// runCLIJSON runs the gigahost CLI in-process with `-o json` prepended,
// capturing stdout/stderr. A non-zero exit is fatal (with stderr). When out is
// non-nil, stdout is JSON-decoded into it.
//
// XDG_CONFIG_HOME is redirected to a throwaway temp dir so a developer's real
// ~/.config/gigahost can never leak into a test run; the token is read from
// GIGAHOST_TOKEN in the environment. Setenv forbids t.Parallel, which is fine:
// every OOB test is serial by nature (it mutates live API state).
func runCLIJSON(t *testing.T, out any, args ...string) {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer

	code := cli.Run(context.Background(), append([]string{"-o", "json"}, args...), cli.Options{
		Version: "oob-acc",
		Commit:  "oob-acc",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	if code != 0 {
		t.Fatalf("cli %v exited %d: %s", args, code, stderr.String())
	}

	if out == nil {
		return
	}

	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		t.Fatalf("cli %v: decode JSON output: %v\n%s", args, err, stdout.String())
	}
}

// ---------------------------------------------------------------------------
// Task 24: DNS zone (the exemplar) + Task 25: import-block variant
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSZone creates a NATIVE zone with the CLI, imports it by
// opaque zone ID, asserts name/type, then applies a matching config (empty
// plan) and destroys it. Requires GIGAHOST_TOKEN and GIGAHOST_TEST_ZONE_APEX.
func TestAccImportOOB_DNSZone(t *testing.T) {
	requireToken(t)

	zoneName := accZoneName(t, "oob-zone")
	client := testAccGigahostClient(t)

	var created struct {
		ID   string
		Name string
		Type string
	}

	runCLIJSON(t, &created, "dns", "zones", "create", zoneName, "--type", "NATIVE")

	if created.ID == "" {
		t.Fatalf("CLI returned empty zone ID for %q", zoneName)
	}

	// Best-effort cleanup runs after Terraform destroy; tolerate not-found
	// (the destroy normally already removed it).
	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBDNSZoneConfig(zoneName)),
			{
				Config:             testAccOOBDNSZoneConfig(zoneName),
				ResourceName:       "gigahost_dns_zone.test",
				ImportState:        true,
				ImportStateId:      created.ID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("name", zoneName),
					checkImportAttr("type", "NATIVE"),
				),
			},
			{
				// Same config must converge: a clean apply with an empty plan
				// proves Terraform now owns the CLI-created zone.
				Config: testAccOOBDNSZoneConfig(zoneName),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_zone.test", "name", zoneName,
				),
			},
		},
	})
}

// TestAccImportOOB_DNSZoneByName is identical to TestAccImportOOB_DNSZone but
// imports by the friendly zone NAME rather than the opaque ID. Requires
// GIGAHOST_TOKEN and GIGAHOST_TEST_ZONE_APEX.
func TestAccImportOOB_DNSZoneByName(t *testing.T) {
	requireToken(t)

	zoneName := accZoneName(t, "oob-zone-name")
	client := testAccGigahostClient(t)

	var created struct {
		ID   string
		Name string
		Type string
	}

	runCLIJSON(t, &created, "dns", "zones", "create", zoneName, "--type", "NATIVE")

	if created.ID == "" {
		t.Fatalf("CLI returned empty zone ID for %q", zoneName)
	}

	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBDNSZoneConfig(zoneName)),
			{
				Config:             testAccOOBDNSZoneConfig(zoneName),
				ResourceName:       "gigahost_dns_zone.test",
				ImportState:        true,
				ImportStateId:      zoneName, // friendly identifier
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("name", zoneName),
					checkImportAttr("type", "NATIVE"),
				),
			},
			{
				Config: testAccOOBDNSZoneConfig(zoneName),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_zone.test", "name", zoneName,
				),
			},
		},
	})
}

// TestAccImportOOB_DNSZoneImportBlock proves the modern `import {}` block flow
// (ImportBlockWithID) produces a clean no-op import for a CLI-created zone.
//
// Unlike the ImportCommandWithID variants, ImportBlockWithID only runs
// `terraform plan` with an import block and asserts the planned action is a
// no-op import — it does not apply or persist, so there is no follow-up Config
// step or framework destroy; cleanup deletes the CLI-created zone directly.
//
// Requires GIGAHOST_TOKEN and GIGAHOST_TEST_ZONE_APEX. If the harness/OpenTofu
// combination rejects import blocks the step fails loudly; switch to t.Skip
// with the failure reason if that proves to be the case in CI.
func TestAccImportOOB_DNSZoneImportBlock(t *testing.T) {
	requireToken(t)

	zoneName := accZoneName(t, "oob-zone-block")
	client := testAccGigahostClient(t)

	var created struct {
		ID   string
		Name string
		Type string
	}

	runCLIJSON(t, &created, "dns", "zones", "create", zoneName, "--type", "NATIVE")

	if created.ID == "" {
		t.Fatalf("CLI returned empty zone ID for %q", zoneName)
	}

	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:          testAccOOBDNSZoneConfig(zoneName),
				ResourceName:    "gigahost_dns_zone.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   created.ID,
			},
		},
	})
}

func testAccOOBDNSZoneConfig(zoneName string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}
`, testAccOOBProviderConfig(), zoneName)
}

// ---------------------------------------------------------------------------
// Task 24: account SSH key (token only)
// ---------------------------------------------------------------------------

// TestAccImportOOB_AccountSSHKey adds an SSH key with the CLI (a freshly
// generated ed25519 key the API will accept), imports it by ID, asserts the
// name, applies a matching config (empty plan), and destroys it. Requires
// GIGAHOST_TOKEN only.
func TestAccImportOOB_AccountSSHKey(t *testing.T) {
	requireToken(t)

	name := accRandName("oob-sshkey")
	pubKey, _ := accEphemeralKey(t)
	client := testAccGigahostClient(t)

	var created struct {
		ID      string
		Name    string
		AddedAt string
	}

	runCLIJSON(t, &created, "account", "ssh-keys", "add", "--name", name, "--data", pubKey)

	if created.ID == "" {
		t.Fatalf("CLI returned empty SSH key ID for %q", name)
	}

	t.Cleanup(func() { _ = client.Account.DeleteSSHKey(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBSSHKeyDestroyed(client, name),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBSSHKeyConfig(name, pubKey)),
			{
				Config:             testAccOOBSSHKeyConfig(name, pubKey),
				ResourceName:       "gigahost_account_ssh_key.test",
				ImportState:        true,
				ImportStateId:      created.ID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("name", name),
				),
			},
			{
				// public_key round-trips through Read now, so the same config
				// converges to an empty plan.
				Config: testAccOOBSSHKeyConfig(name, pubKey),
				Check: resource.TestCheckResourceAttr(
					"gigahost_account_ssh_key.test", "name", name,
				),
			},
		},
	})
}

func testAccOOBSSHKeyConfig(name, pubKey string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}
`, testAccOOBProviderConfig(), name, pubKey)
}

func testAccCheckOOBSSHKeyDestroyed(client *gigahost.Client, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		acc, err := client.Account.Get(accCtx)
		if err != nil {
			return fmt.Errorf("CheckDestroy: get account: %w", err)
		}

		for _, k := range acc.SSHKeys {
			if k.Name == name {
				return fmt.Errorf("SSH key %q still exists after destroy", name)
			}
		}

		return nil
	}
}

// ---------------------------------------------------------------------------
// Task 24: account API key (token only; skips on 403)
// ---------------------------------------------------------------------------

// TestAccImportOOB_AccountAPIKey creates an API key with the CLI, imports it by
// the internal key ID, asserts the label, applies a matching config (empty
// plan), and destroys it. Requires GIGAHOST_TOKEN; skips when the token lacks
// API-key list scope (the create response carries no internal ID without it).
func TestAccImportOOB_AccountAPIKey(t *testing.T) {
	requireToken(t)

	label := accRandName("oob-apikey")
	client := testAccGigahostClient(t)

	// Skip cleanly when the token cannot manage API keys (a 403 on the list
	// endpoint is a scope limitation, not a defect). This mirrors
	// testAccPreCheckAPIKeys but skips on a missing token too.
	if _, err := client.Account.ListAPIKeys(accCtx); err != nil {
		var apiErr *gigahost.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
			t.Skip("token lacks permission to manage API keys; skipping")
		}
	}

	var created struct {
		ID     string
		Prefix string
		Secret string
		Label  string
	}

	runCLIJSON(t, &created, "account", "api-keys", "create", "--label", label)

	// The internal key ID comes from a follow-up list call inside the CLI; if
	// the token cannot list keys it stays empty and we cannot import by ID.
	if created.ID == "" {
		// Best-effort: the key was still created — clean it up by prefix match.
		t.Cleanup(func() { deleteAPIKeyByLabel(client, label) })

		t.Skip("CLI returned empty API key ID (token lacks list scope); cannot import by ID")
	}

	t.Cleanup(func() { _ = client.Account.DeleteAPIKey(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBAPIKeyDestroyed(client, label),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBAPIKeyConfig(label)),
			{
				Config:             testAccOOBAPIKeyConfig(label),
				ResourceName:       "gigahost_account_api_key.test",
				ImportState:        true,
				ImportStateId:      created.ID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("label", label),
				),
			},
			{
				// secret is Computed-only and never returned on Read; the config
				// carries only the label, so the plan converges empty.
				Config: testAccOOBAPIKeyConfig(label),
				Check: resource.TestCheckResourceAttr(
					"gigahost_account_api_key.test", "label", label,
				),
			},
		},
	})
}

func testAccOOBAPIKeyConfig(label string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_api_key" "test" {
  label = %q
}
`, testAccOOBProviderConfig(), label)
}

func testAccCheckOOBAPIKeyDestroyed(client *gigahost.Client, label string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		keys, err := client.Account.ListAPIKeys(accCtx)
		if err != nil {
			return fmt.Errorf("CheckDestroy: list API keys: %w", err)
		}

		for _, k := range keys {
			if k.Label == label {
				return fmt.Errorf("API key %q still exists after destroy", label)
			}
		}

		return nil
	}
}

// deleteAPIKeyByLabel removes any API key matching label (best-effort cleanup).
func deleteAPIKeyByLabel(client *gigahost.Client, label string) {
	keys, err := client.Account.ListAPIKeys(accCtx)
	if err != nil {
		return
	}

	for _, k := range keys {
		if k.Label == label {
			_ = client.Account.DeleteAPIKey(accCtx, k.ID)
		}
	}
}
