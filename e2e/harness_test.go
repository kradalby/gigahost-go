//go:build e2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
	"golang.org/x/crypto/ssh"
)

// skipIfForbidden skips the test when the error is a 403 from the API: the test
// token is a restricted API key and some surfaces (api-keys, /my/*, partner)
// are simply not authorised. That is a token limitation, not a code defect.
func skipIfForbidden(t *testing.T, err error) {
	t.Helper()

	var apiErr *gigahost.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 403 {
		t.Skipf("token lacks permission for this operation: %v", err)
	}
}

// runID is a per-process identifier mixed into every resource name so a test
// run is attributable and the sweeper can recognise its leftovers.
var (
	runIDOnce sync.Once
	runIDVal  string
)

func runID() string {
	runIDOnce.Do(func() {
		// os.Getpid keeps it stable within a run without needing a clock,
		// which the harness forbids in some contexts.
		runIDVal = fmt.Sprintf("%d", os.Getpid())
	})

	return runIDVal
}

// uniqueName returns a collision-resistant, sweeper-recognisable name.
func uniqueName(prefix string) string {
	return fmt.Sprintf("go-e2e-%s-%s", prefix, runID())
}

// newClient builds a live client from GIGAHOST_TOKEN, skipping the test when it
// is absent so the suite degrades gracefully outside CI/dev.
func newClient(t *testing.T) *gigahost.Client {
	t.Helper()

	token := os.Getenv("GIGAHOST_TOKEN")
	if token == "" {
		t.Skip("GIGAHOST_TOKEN not set; skipping live e2e test")
	}

	c, err := gigahost.NewClient(gigahost.WithToken(token))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	return c
}

func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return ctx
}

// ephemeralKey generates a throwaway ed25519 keypair, returning the public key
// in authorized_keys form (to register with the account) and a signer for SSH
// login. The private key never touches disk.
func ephemeralKey(t *testing.T) (authorizedKey string, signer ssh.Signer) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ephemeralKey: generate: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ephemeralKey: public key: %v", err)
	}

	signer, err = ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ephemeralKey: signer: %v", err)
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), signer
}

// findSSHKeyID returns the ID of the account SSH key with the given name, or ""
// if absent. Uses a fresh context so it is safe to call from t.Cleanup.
func findSSHKeyID(c *gigahost.Client, name string) string {
	acc, err := c.Account.Get(context.Background())
	if err != nil {
		return ""
	}

	for _, k := range acc.SSHKeys {
		if k.Name == name {
			return k.ID
		}
	}

	return ""
}

// findAPIKeyIDByPrefix returns the ID of the API key with the given prefix, or
// "" if absent.
func findAPIKeyIDByPrefix(c *gigahost.Client, prefix string) string {
	keys, err := c.Account.ListAPIKeys(context.Background())
	if err != nil {
		return ""
	}

	for _, k := range keys {
		if k.Prefix == prefix {
			return k.ID
		}
	}

	return ""
}

// pickOS resolves an os_id to install, preferring a Debian image and falling
// back to the first OS of the first active distribution.
func pickOS(t *testing.T, c *gigahost.Client) string {
	t.Helper()

	ctx := testContext(t)

	dists, err := c.Reinstall.ListDistributions(ctx)
	if err != nil {
		t.Fatalf("ListDistributions: %v", err)
	}

	distID := ""

	for _, d := range dists {
		if strings.EqualFold(d.Name, "Debian") {
			distID = d.ID

			break
		}
	}

	if distID == "" && len(dists) > 0 {
		distID = dists[0].ID
	}

	if distID == "" {
		t.Fatal("pickOS: no distributions available")
	}

	oses, err := c.Reinstall.ListOperatingSystems(ctx, distID)
	if err != nil {
		t.Fatalf("ListOperatingSystems: %v", err)
	}

	if len(oses) == 0 {
		t.Fatalf("pickOS: no OSes for distribution %s", distID)
	}

	return oses[0].ID
}

// pollDeployReady polls /deploy/status until all servers are ready, returning
// the first server's status. It fails the test if the timeout elapses.
func pollDeployReady(t *testing.T, c *gigahost.Client, orderIDs []string, timeout time.Duration) gigahost.DeployServerStatus {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		st, err := c.Deploy.GetStatus(ctx, orderIDs)
		if err != nil {
			t.Fatalf("GetStatus: %v", err)
		}

		if st.AllReady && len(st.Servers) > 0 {
			return st.Servers[0]
		}

		select {
		case <-ctx.Done():
			t.Fatalf("deploy not ready within %s (last: %+v)", timeout, st.Servers)
		case <-ticker.C:
		}
	}
}

// sshRun connects to host:22 as root using signer and runs cmd, returning its
// stdout. It retries the initial dial because sshd may not be up the instant the
// server reports ready.
func sshRun(t *testing.T, host string, signer ssh.Signer, cmd string) string {
	t.Helper()

	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // fresh ephemeral host
		Timeout:         15 * time.Second,
	}

	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastErr error

	for {
		client, err := ssh.Dial("tcp", host+":22", cfg)
		if err == nil {
			defer client.Close()

			sess, serr := client.NewSession()
			if serr != nil {
				t.Fatalf("ssh new session: %v", serr)
			}
			defer sess.Close()

			out, rerr := sess.Output(cmd)
			if rerr != nil {
				t.Fatalf("ssh run %q: %v", cmd, rerr)
			}

			return string(out)
		}

		lastErr = err

		select {
		case <-deadline.C:
			t.Fatalf("ssh dial %s:22 failed within timeout: %v", host, lastErr)
		case <-ticker.C:
		}
	}
}

// apiKeyExists reports whether an API key with the given ID is still present.
func apiKeyExists(c *gigahost.Client, id string) bool {
	keys, err := c.Account.ListAPIKeys(context.Background())
	if err != nil {
		return false
	}

	for _, k := range keys {
		if k.ID == id {
			return true
		}
	}

	return false
}

// deployTarget is the resolved cheapest product plus a region to deploy it in.
type deployTarget struct {
	ProductID string
	PriceID   string
	RegionID  string
	Product   gigahost.DeployProduct
}

// cheapestTarget reads the catalog and returns the cheapest product (lowest
// hourly rate) that carries a price ID and at least one region. The optional
// GIGAHOST_TEST_PRODUCT env var pins a specific product ID for debugging.
func cheapestTarget(t *testing.T, c *gigahost.Client) deployTarget {
	t.Helper()

	cat, err := c.Deploy.GetCatalog(testContext(t))
	if err != nil {
		t.Fatalf("cheapestTarget: GetCatalog: %v", err)
	}

	pin := os.Getenv("GIGAHOST_TEST_PRODUCT")

	var (
		best   *gigahost.DeployProduct
		bestPx gigahost.DeployProduct
	)

	for _, tier := range cat.Tiers {
		for _, p := range tier.Products {
			if p.PriceID == "" || len(p.RegionIDs) == 0 {
				continue
			}

			if pin != "" && p.ID != pin {
				continue
			}

			if best == nil || p.RateHourly < best.RateHourly {
				bestPx = p
				best = &bestPx
			}
		}
	}

	if best == nil {
		t.Fatal("cheapestTarget: no deployable product found in catalog")
	}

	return deployTarget{
		ProductID: best.ID,
		PriceID:   best.PriceID,
		RegionID:  best.RegionIDs[0],
		Product:   *best,
	}
}
