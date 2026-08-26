package tfprovider_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
func accEphemeralKey(t *testing.T) (string, ssh.Signer) {
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

// accPickOS resolves an OS slug, preferring Debian.
func accPickOS(t *testing.T, c *gigahost.Client) string {
	t.Helper()

	all, err := c.Reinstall.ListAllOperatingSystems(accCtx)
	if err != nil {
		t.Fatalf("accPickOS: %v", err)
	}

	if len(all) == 0 {
		t.Fatal("accPickOS: no operating systems")
	}

	for _, o := range all {
		if strings.EqualFold(o.Distribution.Value, "debian") {
			return o.Slug
		}
	}

	return all[0].Slug
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

// accSSHLogin reads the server's ip from state and logs in with signer, running
// a command to prove the managed key was injected. It retries the dial because
// sshd may not be up the instant the server reports ready.
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

		cfg := &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
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
