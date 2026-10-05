package tfprovider_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"golang.org/x/crypto/ssh"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestAccServer_deployAndSSH is the flagship: it deploys the cheapest server
// referencing a Terraform-managed SSH key (exercising the dependency graph),
// logs in over SSH to prove the key was injected, then destroys (cancels) the
// server and confirms it was cancelled.
func TestAccServer_deployAndSSH(t *testing.T) {
	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osSlug := accPickOS(t, client)
	pub, signer := accEphemeralKey(t)
	sshName := accRandName("srv-deploy")
	hostname := accRandName("tfacc") + ".example.com"

	var serverID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckServerCancelled(client, &serverID),
		Steps: []resource.TestStep{
			{
				// region is intentionally omitted: with one live region the
				// resource must auto-resolve and record it.
				Config: testAccServerConfigHost(sshName, pub, typeSlug, sizeSlug, osSlug, hostname),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_server.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "ip"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "primary_ip_id"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "cores"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "region"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "memory_gb"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "rate_hourly"),
					resource.TestCheckResourceAttr("gigahost_server.test", "platform", "cloud"),
					captureAttr("gigahost_server.test", "id", &serverID),
					accSSHLogin("gigahost_server.test", signer),
					// GET /servers lags behind a fresh deploy by a minute or
					// two; the by_hostname lookup in step 2 needs the server
					// listed, so wait for it (and confirm the hostname the
					// API actually stores).
					testAccWaitServerListed(client, &serverID, hostname),
				),
			},
			{
				// Import the just-deployed server by ID and verify Read
				// converges. ImportStateVerify compares the freshly imported
				// state against the state from the deploy step; the ignores
				// below are exactly the attributes Read cannot recover from the
				// live API (so they would be null after import and mismatch the
				// deploy-time values). Crucially os is NOT ignored: Read
				// resolves the live OSID back to its catalog slug, which is the
				// proof that Read converges os. (The Update adoption branch is
				// not exercised here — the OOB import test covers it.)
				ResourceName:      "gigahost_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					// Product selectors: the live API reports product_id "0",
					// so Read leaves them null; they are adopted from config at
					// the first apply, not recovered on import.
					"type",
					"size",
					"platform",
					// Deploy-time-only inputs the API never reports back.
					"ssh_keys",
					"backups",
					"iso",
					"rescue",
					// Returned once at deploy and never again.
					"password",
					// The deploy order is not linked from the server record
					// (upstream B12), so Read cannot recover it on import.
					"order_id",
					// Deploy-time catalog facts; Read does not repopulate them.
					"memory_gb",
					"storage_gb",
					"rate_hourly",
					"rate_monthly",
					// hostname may land in srv_name rather than srv_hostname, so
					// recovery from srv.Hostname is not guaranteed; the ignore stays.
					"hostname",
					// region is matched from Location best-effort; Location is a
					// different namespace than catalog region IDs, so a match is
					// not guaranteed.
					"region",
				},
			},
			{
				// Piggyback the server data sources on the live server so
				// they get acceptance coverage without a second deploy —
				// including the hostname lookup and the ips list.
				Config: testAccServerConfigHost(sshName, pub, typeSlug, sizeSlug, osSlug, hostname) + fmt.Sprintf(`
data "gigahost_server" "by_id" {
  id = gigahost_server.test.id
}

data "gigahost_server" "by_hostname" {
  hostname   = %q
  depends_on = [gigahost_server.test]
}

data "gigahost_servers" "all" {
  depends_on = [gigahost_server.test]
}
`, hostname),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.gigahost_server.by_id", "id", "gigahost_server.test", "id"),
					resource.TestCheckResourceAttrSet("data.gigahost_server.by_id", "primary_ip"),
					resource.TestCheckResourceAttrPair("data.gigahost_server.by_hostname", "id", "gigahost_server.test", "id"),
					resource.TestCheckResourceAttrSet("data.gigahost_server.by_id", "ips.0.id"),
					resource.TestCheckResourceAttrSet("data.gigahost_servers.all", "servers.0.id"),
				),
			},
		},
	})
}

func testAccServerConfig(sshName, pubKey, typeSlug, sizeSlug, osSlug string) string {
	return testAccServerConfigHost(sshName, pubKey, typeSlug, sizeSlug, osSlug, "")
}

// testAccWaitServerListed polls GET /servers until the deployed server shows
// up (the list lags a fresh deploy), then verifies the stored hostname
// matches what was requested — the by_hostname data source lookup depends
// on both.
func testAccWaitServerListed(client *gigahost.Client, serverID *string, wantHostname string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		deadline := time.Now().Add(5 * time.Minute)

		var lastHostnames []string

		for time.Now().Before(deadline) {
			servers, err := client.Servers.List(accCtx)
			if err != nil {
				return fmt.Errorf("list servers: %w", err)
			}

			lastHostnames = lastHostnames[:0]

			for _, s := range servers {
				lastHostnames = append(lastHostnames, s.Hostname, s.Name)

				if s.ID != *serverID {
					continue
				}

				// The deploy hostname lands in srv_name (srv_hostname stays
				// empty) — Resolve matches both.
				if !strings.EqualFold(s.Hostname, wantHostname) && !strings.EqualFold(s.Name, wantHostname) {
					return fmt.Errorf("server %s listed with hostname %q / name %q, want %q — by_hostname lookups would miss it",
						s.ID, s.Hostname, s.Name, wantHostname)
				}

				return nil
			}

			time.Sleep(15 * time.Second)
		}

		return fmt.Errorf("server %s never appeared in GET /servers within 5m (listed hostnames: %v)",
			*serverID, lastHostnames)
	}
}

// testAccServerConfigHost is testAccServerConfig with an explicit hostname
// (empty omits the attribute).
func testAccServerConfigHost(sshName, pubKey, typeSlug, sizeSlug, osSlug, hostname string) string {
	hostnameLine := ""
	if hostname != "" {
		hostnameLine = fmt.Sprintf("  hostname = %q\n", hostname)
	}

	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}

resource "gigahost_server" "test" {
  type     = %q
  size     = %q
  os       = %q
%s  ssh_keys = [gigahost_account_ssh_key.test.id]
}
`, testAccProviderConfig(), sshName, pubKey, typeSlug, sizeSlug, osSlug, hostnameLine)
}

// accEphemeralKey generates a throwaway ed25519 keypair for an acceptance test.
func accEphemeralKey(t *testing.T, keyFiles ...string) (string, ssh.Signer) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("accEphemeralKey: generate: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("accEphemeralKey: public key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("accEphemeralKey: signer: %v", err)
	}

	if len(keyFiles) > 0 {
		block, err := ssh.MarshalPrivateKey(priv, "gigahost acceptance matrix")
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(keyFiles[0], pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), signer
}

// accCheapestTarget resolves the cheapest deployable cloud product and
// returns its (type, size) slugs — never hardcoded catalog values.
func accCheapestTarget(t *testing.T, c *gigahost.Client) (string, string) {
	t.Helper()

	// The cheapest product is sometimes out of stock (the catalog exposes no
	// stock signal; the deploy 400s at order time). GIGAHOST_TEST_DEPLOY_TYPE /
	// GIGAHOST_TEST_DEPLOY_SIZE override the target with a known in-stock size.
	if ts, ss := os.Getenv("GIGAHOST_TEST_DEPLOY_TYPE"), os.Getenv("GIGAHOST_TEST_DEPLOY_SIZE"); ts != "" && ss != "" {
		return ts, ss
	}

	cat, err := c.Deploy.GetCatalog(accCtx)
	if err != nil {
		t.Fatalf("accCheapestTarget: GetCatalog: %v", err)
	}

	// VMs only: a dedicated box costs far more and provisions far slower than
	// these tests need. GIGAHOST_TEST_DEPLOY_TYPE/_SIZE above is the opt-out.
	best, err := cat.FindProduct(gigahost.ProductSelector{
		Platform: gigahost.PlatformCloud,
		Cheapest: true,
	})
	if err != nil {
		t.Fatalf("accCheapestTarget: %v", err)
	}

	typeSlug := ""

	for i := range cat.Tiers {
		for j := range cat.Tiers[i].Products {
			if cat.Tiers[i].Products[j].ID == best.ID {
				typeSlug = cat.Tiers[i].TypeSlug()
			}
		}
	}

	if typeSlug == "" {
		t.Fatalf("accCheapestTarget: no tier for product %s", best.ID)
	}

	return typeSlug, best.SizeSlug()
}

// accPickOS resolves the newest Debian: the catalog keeps end-of-life
// releases whose installers no longer complete.
func accPickOS(t *testing.T, c *gigahost.Client) string {
	t.Helper()

	return accPickDistro(t, c, "debian")
}

// accPickDistro resolves the newest OS slug of one distribution, skipping the
// test when the catalog does not offer it.
func accPickDistro(t *testing.T, c *gigahost.Client, distro string) string {
	t.Helper()

	all, err := c.Reinstall.ListAllOperatingSystems(accCtx)
	if err != nil {
		t.Fatalf("accPickDistro: %v", err)
	}

	slug := ""

	for _, o := range all {
		if strings.EqualFold(o.Distribution.Value, distro) {
			slug = o.Slug
		}
	}

	if slug == "" {
		t.Skipf("catalog offers no %s", distro)
	}

	return slug
}

// accMatrixKey registers a generated key or reuses a supplied registered key.
func accMatrixKey(t *testing.T, client *gigahost.Client, matrixDir string, keepFailures bool) (string, ssh.Signer, string, string) {
	t.Helper()

	keyFile := os.Getenv("GIGAHOST_TEST_MATRIX_KEY_FILE")

	var (
		pub    string
		signer ssh.Signer
	)
	if keyFile == "" {
		pub, signer = accEphemeralKey(t, filepath.Join(matrixDir, "private", "id_ed25519"))
	} else {
		body, err := os.ReadFile(keyFile)
		if err != nil {
			t.Fatal(err)
		}

		signer, err = ssh.ParsePrivateKey(body)
		if err != nil {
			t.Fatal(err)
		}

		pub = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	}

	keyName := accRandName("os-matrix")
	if keyFile == "" {
		if err := client.Account.AddSSHKey(accCtx, keyName, pub); err != nil {
			t.Fatal(err)
		}
	}
	// Parent cleanup runs after all parallel cells, even if key lookup fails.
	t.Cleanup(func() {
		// A supplied key belongs to an earlier run and may still have live VMs.
		if keyFile != "" {
			return
		}

		account, err := client.Account.Get(accCtx)
		if err != nil {
			t.Errorf("lookup matrix key for cleanup: %v", err)

			return
		}

		for _, key := range account.SSHKeys {
			if key.Name == keyName {
				if keepFailures && t.Failed() {
					t.Logf("retained shared SSH key %s for failed VMs", key.ID)

					return
				}

				if err := client.Account.DeleteSSHKey(accCtx, key.ID); err != nil {
					t.Errorf("delete matrix key %s: %v", key.ID, err)
				}
			}
		}
	})

	account, err := client.Account.Get(accCtx)
	if err != nil {
		t.Fatal(err)
	}

	keyID := ""

	for _, key := range account.SSHKeys {
		if key.Name == keyName || (keyFile != "" && strings.TrimSpace(key.Data) == pub) {
			keyID = key.ID
			keyName = key.Name
		}
	}

	if keyID == "" {
		t.Fatal("matrix SSH key not found on account")
	}

	return pub, signer, keyID, keyName
}

func TestMatrixSuppliedKey(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "id_ed25519")
	pub, _ := accEphemeralKey(t, keyFile)
	t.Setenv("GIGAHOST_TEST_MATRIX_KEY_FILE", keyFile)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/account" {
			t.Errorf("supplied key must not be created or deleted: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		fmt.Fprintf(w, `{"data":{"sshkeys":[{"key_id":"42","key_name":"original","key_data":%q}]}}`, pub)
	}))
	t.Cleanup(api.Close)

	client, err := gigahost.NewClient(gigahost.WithBaseURL(api.URL), gigahost.WithToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}

	got, signer, id, name := accMatrixKey(t, client, dir, false)
	if got != pub || id != "42" || name != "original" || strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) != pub {
		t.Fatal("supplied key was not reused")
	}
}

// TestAccServer_perOS is the opt-in fresh-install OS × SSH-auth matrix.
// Inputs are selected once; every cell gets its own VM and Terraform state.
func TestAccServer_perOS(t *testing.T) {
	testAccRequireEnv(t, "GIGAHOST_TEST_OS_MATRIX")
	t.Parallel()

	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	keepFailures := os.Getenv("GIGAHOST_TEST_KEEP_FAILED") == "1"

	matrixDir := os.Getenv("GIGAHOST_TEST_MATRIX_DIR")
	if matrixDir == "" {
		matrixDir = filepath.Join("..", ".direnv", "deploy-matrix-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}

	matrixDir, err := filepath.Abs(matrixDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(matrixDir, "private"), 0o700); err != nil {
		t.Fatal(err)
	}

	catalog, err := client.Deploy.GetCatalog(accCtx)
	if err != nil {
		t.Fatal(err)
	}

	product, err := catalog.FindProduct(gigahost.ProductSelector{Platform: gigahost.PlatformCloud, Type: typeSlug, Size: sizeSlug})
	if err != nil {
		t.Fatal(err)
	}

	region, err := catalog.RegionForProduct(product, "")
	if err != nil {
		t.Fatal(err)
	}

	all, err := client.Reinstall.ListAllOperatingSystems(accCtx)
	if err != nil {
		t.Fatal(err)
	}

	pub, signer, keyID, keyName := accMatrixKey(t, client, matrixDir, keepFailures)

	t.Logf("inputs: type=%s size=%s region=%s ssh_key_id=%s", typeSlug, sizeSlug, region.Slug(), keyID)
	t.Logf("matrix evidence: %s", matrixDir)

	inputs, err := json.Marshal(map[string]any{
		"type": typeSlug, "size": sizeSlug, "region": region.Slug(),
		"ssh_key_id": keyID, "ssh_key_name": keyName, "ssh_public_key": pub,
		"keep_failed": keepFailures,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(matrixDir, "inputs.json"), inputs, 0o600); err != nil {
		t.Fatal(err)
	}

	// Created 2026-10-05 from the test-account catalog. Keep the two newest
	// Debian/Ubuntu releases and the latest AlmaLinux/RockyLinux/Fedora.
	// Refresh the pinned slugs with: direnv exec . nix run .#gigahost -- deploy os
	// Family-only entries resolve their latest cloud amd64 version at run time.
	for _, target := range []string{"debian-12", "debian-13", "ubuntu-24.04", "ubuntu-26.04", "almalinux", "rockylinux", "fedora"} {
		image := accMatrixOS(all, target)

		osSlug := target
		if image != nil {
			osSlug = image.Slug
		}

		for _, auth := range []struct {
			name   string
			signer ssh.Signer
		}{{"key", signer}, {"password", nil}} {
			t.Run(osSlug+"/"+auth.name, func(t *testing.T) {
				t.Parallel()

				if image == nil {
					t.Skipf("catalog offers no cloud amd64 image for %s", target)
				}

				keys := "[]"
				if auth.signer != nil {
					keys = fmt.Sprintf("[%q]", keyID)
				}

				caseDir := filepath.Join(matrixDir, osSlug, auth.name)
				keep := func() bool { return keepFailures && t.Failed() }
				apiURL := accMatrixAPI(t, client.BaseURL(), caseDir, keep)

				config := fmt.Sprintf(`
%s
resource "gigahost_server" "test" {
  type     = %q
  size     = %q
  region   = %q
  os       = %q
  hostname = %q
  ssh_keys = %s
}
`, testAccProviderConfig(), typeSlug, sizeSlug, region.Slug(), osSlug, accRandName("os-matrix"), keys)
				if err := os.WriteFile(filepath.Join(caseDir, "share", "main.tf"), []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}

				var serverID string
				resource.Test(t, resource.TestCase{
					PreCheck:                 func() { testAccPreCheck(t) },
					ProtoV6ProviderFactories: testAccProviderFactories,
					CheckDestroy: func(s *terraform.State) error {
						if keep() {
							return nil
						}

						return testAccCheckServerCancelled(client, &serverID)(s)
					},
					ErrorCheck: func(err error) error {
						if writeErr := os.WriteFile(filepath.Join(caseDir, "private", "error.txt"), []byte(err.Error()), 0o600); writeErr != nil {
							t.Errorf("save matrix error: %v", writeErr)
						}

						return err
					},
					Steps: []resource.TestStep{{
						Config: strings.Replace(config, testAccProviderConfig(), fmt.Sprintf(`provider "gigahost" { base_url = %q }`, apiURL), 1),
						Check: resource.ComposeAggregateTestCheckFunc(
							captureAttr("gigahost_server.test", "id", &serverID),
							resource.TestCheckResourceAttr("gigahost_server.test", "status", "running"),
							accSSHLogin("gigahost_server.test", auth.signer),
						),
					}},
				})
			})
		}
	}
}

// captureAttr records a resource attribute value into dst during a test step.
func captureAttr(name, attr string, dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}

		*dst = rs.Primary.Attributes[attr]

		return nil
	}
}

// accSSHLogin proves root SSH access by running hostname. A nil signer uses
// only the password in state; it never falls back to another auth method.
func accSSHLogin(name string, signer ssh.Signer) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}

		ip := rs.Primary.Attributes["ip"]
		if ip == "" {
			return fmt.Errorf("resource %s has no ip attribute", name)
		}

		var auth ssh.AuthMethod
		if signer != nil {
			auth = ssh.PublicKeys(signer)
		} else {
			password := rs.Primary.Attributes["password"]
			if password == "" {
				return fmt.Errorf("resource %s has no root password in state", name)
			}

			auth = ssh.Password(password)
		}

		cfg := &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         15 * time.Second,
		}

		deadline := time.Now().Add(3 * time.Minute)

		var lastErr error

		for time.Now().Before(deadline) {
			client, derr := ssh.Dial("tcp", ip+":22", cfg)
			if derr != nil {
				lastErr = derr

				time.Sleep(10 * time.Second)

				continue
			}

			defer client.Close()

			sess, serr := client.NewSession()
			if serr != nil {
				return fmt.Errorf("ssh session: %w", serr)
			}
			defer sess.Close()

			out, rerr := sess.Output("hostname")
			if rerr != nil {
				return fmt.Errorf("ssh run hostname: %w", rerr)
			}

			if strings.TrimSpace(string(out)) == "" {
				return errors.New("ssh hostname returned empty output")
			}

			return nil
		}

		return fmt.Errorf("ssh dial %s:22 failed within timeout: %w", ip, lastErr)
	}
}

// testAccCheckServerCancelled verifies, after destroy, that the server was
// cancelled: a repeat cancel must fail (the order is already gone).
func testAccCheckServerCancelled(client *gigahost.Client, serverID *string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if *serverID == "" {
			return nil
		}

		if err := client.Servers.Cancel(accCtx, *serverID); err == nil {
			return fmt.Errorf("server %s was still cancellable after destroy; Delete may not have cancelled it", *serverID)
		}

		return nil
	}
}

// accMatrixOS resolves an exact slug or the newest cloud amd64 OS of a family.
func accMatrixOS(all []gigahost.ResolvedOS, target string) *gigahost.ResolvedOS {
	var (
		latest        *gigahost.ResolvedOS
		latestVersion *version.Version
	)

	for i := range all {
		o := &all[i]
		if o.OS.DedicatedOnly || o.OS.Arch != "amd64" {
			continue
		}

		if o.Slug == target {
			return o
		}

		if !strings.EqualFold(o.Distribution.Value, target) {
			continue
		}

		_, release, _ := strings.Cut(o.Slug, "-")
		release, _, _ = strings.Cut(release, "-")

		v, err := version.NewVersion(release)
		if err == nil && (latestVersion == nil || v.GreaterThan(latestVersion)) {
			latest, latestVersion = o, v
		}
	}

	return latest
}

func TestDeploymentMatrixOS(t *testing.T) {
	all := []gigahost.ResolvedOS{
		{Slug: "almalinux-9", Distribution: gigahost.Distribution{Value: "almalinux"}, OS: gigahost.ReinstallOS{Arch: "amd64"}},
		{Slug: "almalinux-10", Distribution: gigahost.Distribution{Value: "almalinux"}, OS: gigahost.ReinstallOS{Arch: "amd64"}},
		{Slug: "almalinux-8", Distribution: gigahost.Distribution{Value: "almalinux"}, OS: gigahost.ReinstallOS{Arch: "amd64"}},
		{Slug: "almalinux-11", Distribution: gigahost.Distribution{Value: "almalinux"}, OS: gigahost.ReinstallOS{Arch: "amd64", DedicatedOnly: true}},
		{Slug: "almalinux-12", Distribution: gigahost.Distribution{Value: "almalinux"}, OS: gigahost.ReinstallOS{Arch: "arm64"}},
		{Slug: "fedora-43-server", Distribution: gigahost.Distribution{Value: "fedora"}, OS: gigahost.ReinstallOS{Arch: "amd64"}},
	}

	for _, tc := range []struct{ target, want string }{
		{"almalinux", "almalinux-10"},
		{"almalinux-9", "almalinux-9"},
		{"fedora", "fedora-43-server"},
		{"rockylinux", ""},
		{"almalinux-11", ""},
		{"almalinux-12", ""},
	} {
		t.Run(tc.target, func(t *testing.T) {
			got := ""
			if os := accMatrixOS(all, tc.target); os != nil {
				got = os.Slug
			}

			if got != tc.want {
				t.Fatalf("resolved %q, want %q", got, tc.want)
			}
		})
	}
}
