//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestDeployServerSSHLogin is the headline scenario (the user's "basic server"
// example): deploy the cheapest server with a generated SSH key, wait for it to
// become ready, log in over SSH and run a command, then cancel the server and
// confirm it is gone.
func TestDeployServerSSHLogin(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	target := cheapestTarget(t, c)
	osID := pickOS(t, c)
	pub, signer := ephemeralKey(t)
	keyName := uniqueName("deploykey")

	if err := c.Account.AddSSHKey(ctx, keyName, pub); err != nil {
		skipIfForbidden(t, err)
		t.Fatalf("AddSSHKey: %v", err)
	}

	t.Cleanup(func() {
		if id := findSSHKeyID(c, keyName); id != "" {
			_ = c.Account.DeleteSSHKey(context.Background(), id)
		}
	})

	keyID := findSSHKeyID(c, keyName)
	if keyID == "" {
		t.Fatalf("ssh key %q not found after add", keyName)
	}

	resp, err := c.Deploy.Deploy(ctx, gigahost.DeployServerRequest{
		ProductID: target.ProductID,
		PriceID:   target.PriceID,
		RegionID:  target.RegionID,
		OSID:      osID,
		Quantity:  1,
		SSHKeys:   []string{keyID},
	})
	if err != nil {
		skipIfForbidden(t, err)
		t.Fatalf("Deploy: %v", err)
	}

	if len(resp.OrderIDs) == 0 {
		t.Fatal("Deploy returned no order IDs")
	}

	// Safety-net teardown: cancel the server even if the test aborts before
	// polling resolves its ID, by re-resolving from the order.
	var serverID string

	t.Cleanup(func() {
		sid := serverID
		if sid == "" {
			if st, gerr := c.Deploy.GetStatus(context.Background(), resp.OrderIDs); gerr == nil && len(st.Servers) > 0 {
				sid = st.Servers[0].ServerID
			}
		}

		if sid != "" {
			_ = c.Servers.Cancel(context.Background(), sid)
		}
	})

	t.Logf("deploying %s (product %s) in region %s, os %s",
		target.Product.Name, target.ProductID, target.RegionID, osID)

	st := pollDeployReady(t, c, resp.OrderIDs, 10*time.Minute)
	serverID = st.ServerID

	if serverID == "" || st.IP == "" {
		t.Fatalf("server ready but missing id/ip: %+v", st)
	}

	t.Logf("server %s ready at %s; logging in over SSH", serverID, st.IP)

	host := sshRun(t, st.IP, signer, "hostname")
	if strings.TrimSpace(host) == "" {
		t.Error("ssh hostname returned empty output")
	}

	t.Logf("ssh login OK, remote hostname: %q", strings.TrimSpace(host))

	// Explicit teardown. Removal from the server list is asynchronous, so the
	// reliable confirmation is that a second cancel reports the order is gone.
	if err := c.Servers.Cancel(ctx, serverID); err != nil {
		t.Fatalf("Servers.Cancel: %v", err)
	}

	if err := c.Servers.Cancel(ctx, serverID); err == nil {
		t.Errorf("second cancel of %s succeeded; expected already-cancelled error", serverID)
	}

	serverID = "" // cancelled; disarm the safety-net cleanup
}
