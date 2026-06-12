//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestGrandTour deploys one server and exercises the major server actions in
// sequence, logging the behaviours that drive the Terraform lifecycle design:
// does reinstall keep the same IP? how do power transitions report? does
// ordering an extra IPv4 add to the server? It is intentionally lenient —
// operations must not error, and the interesting facts are logged.
func TestGrandTour(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	target := cheapestTarget(t, c)
	osID := pickOS(t, c)
	pub, signer := ephemeralKey(t)
	keyName := uniqueName("tourkey")

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

	ready := pollDeployReady(t, c, resp.OrderIDs, 10*time.Minute)
	serverID = ready.ServerID
	ipBefore := ready.IP
	t.Logf("[deploy] server=%s ip=%s", serverID, ipBefore)

	if out := sshRun(t, ready.IP, signer, "hostname"); out != "" {
		t.Logf("[ssh] initial login OK: %q", out)
	}

	// ---- rename (UpdateName) ----
	newLabel := uniqueName("tour-label")
	if err := c.Servers.UpdateName(ctx, serverID, newLabel); err != nil {
		t.Errorf("[rename] UpdateName: %v", err)
	} else if srv, gerr := c.Servers.Get(ctx, serverID); gerr == nil {
		t.Logf("[rename] label now %q (in-place, no rebuild)", srv.Label)
	}

	// ---- reverse DNS on primary IPv4 ----
	if srv, gerr := c.Servers.Get(ctx, serverID); gerr == nil {
		for _, ip := range srv.IPs {
			if ip.Version == "4" && ip.ID != "" {
				err := c.Servers.UpdateReverse(ctx, serverID, gigahost.UpdateReverseRequest{
					IPID: ip.ID,
					DNS:  "tour." + uniqueName("rdns") + ".example.com",
				})
				t.Logf("[reverse] set rDNS on ip_id=%s err=%v", ip.ID, err)

				break
			}
		}
	}

	// ---- order an extra IPv4 ----
	ipCountBefore := countIPs(t, c, serverID)
	if err := c.Servers.OrderIPv4(ctx, serverID, gigahost.IPTypeL3); err != nil {
		t.Logf("[extra-ipv4] OrderIPv4 err=%v", err)
	} else {
		t.Logf("[extra-ipv4] ordered; ip count before=%d after=%d", ipCountBefore, countIPs(t, c, serverID))
	}

	// ---- snapshot lifecycle ----
	snapName := uniqueName("snap")
	if err := c.Snapshots.Create(ctx, serverID, snapName); err != nil {
		t.Errorf("[snapshot] Create: %v", err)
	} else {
		snap := pollSnapshot(t, c, serverID, snapName, 5*time.Minute)
		if snap != nil {
			t.Logf("[snapshot] created id=%d state=%s", snap.ID, snap.State)

			if err := c.Snapshots.Delete(ctx, serverID, snap.ID); err != nil {
				t.Errorf("[snapshot] Delete: %v", err)
			} else {
				t.Logf("[snapshot] deleted")
			}
		}
	}

	// ---- power cycle ----
	if err := c.Servers.PowerOff(ctx, serverID); err != nil {
		t.Logf("[power] PowerOff err=%v", err)
	} else {
		t.Logf("[power] off state=%v", pollPower(t, c, serverID, false, 3*time.Minute))
	}

	if err := c.Servers.PowerOn(ctx, serverID); err != nil {
		t.Logf("[power] PowerOn err=%v", err)
	} else {
		t.Logf("[power] on state=%v", pollPower(t, c, serverID, true, 3*time.Minute))
	}

	if err := c.Servers.Reboot(ctx, serverID); err != nil {
		t.Logf("[power] Reboot err=%v", err)
	}

	// ---- reinstall (the key lifecycle question: is the IP preserved?) ----
	altOS := pickOSExcept(t, c, osID)
	res, err := c.Reinstall.Reinstall(ctx, serverID, gigahost.ReinstallRequest{OSID: altOS})
	if err != nil {
		t.Logf("[reinstall] err=%v", err)
	} else {
		t.Logf("[reinstall] initiated os=%s reboot=%v", altOS, res.Reboot)

		// Give the reinstall time, then read the server back and compare IP.
		waitForInstallDone(t, c, serverID, 8*time.Minute)

		if srv, gerr := c.Servers.Get(ctx, serverID); gerr == nil {
			t.Logf("[reinstall] server_id stable=%v ip_before=%s ip_after=%s ip_preserved=%v",
				srv.ID == serverID, ipBefore, srv.PrimaryIP, srv.PrimaryIP == ipBefore)
		}
	}

	// ---- teardown ----
	if err := c.Servers.Cancel(ctx, serverID); err != nil {
		t.Fatalf("[cancel] %v", err)
	}

	if err := c.Servers.Cancel(ctx, serverID); err == nil {
		t.Errorf("[cancel] second cancel succeeded; expected already-cancelled")
	}

	serverID = ""
}

func countIPs(t *testing.T, c *gigahost.Client, serverID string) int {
	t.Helper()

	srv, err := c.Servers.Get(context.Background(), serverID)
	if err != nil {
		return -1
	}

	return len(srv.IPs)
}

func pollSnapshot(t *testing.T, c *gigahost.Client, serverID, name string, timeout time.Duration) *gigahost.Snapshot {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		snaps, err := c.Snapshots.List(ctx, serverID)
		if err == nil {
			for i := range snaps {
				// The user-supplied name is stored as the display name; snap_name
				// is a random internal identifier.
				if snaps[i].DisplayName == name {
					return &snaps[i]
				}
			}
		}

		select {
		case <-ctx.Done():
			t.Logf("[snapshot] %q not found within %s", name, timeout)

			return nil
		case <-ticker.C:
		}
	}
}

func pollPower(t *testing.T, c *gigahost.Client, serverID string, want bool, timeout time.Duration) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		ps, err := c.Servers.GetPowerState(ctx, serverID)
		if err == nil && ps.PowerState == want {
			return ps.PowerState
		}

		select {
		case <-ctx.Done():
			if ps, err := c.Servers.GetPowerState(context.Background(), serverID); err == nil {
				return ps.PowerState
			}

			return !want
		case <-ticker.C:
		}
	}
}

func waitForInstallDone(t *testing.T, c *gigahost.Client, serverID string, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		srv, err := c.Servers.Get(ctx, serverID)
		if err == nil && !srv.StatusInstall {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pickOSExcept(t *testing.T, c *gigahost.Client, excludeOSID string) string {
	t.Helper()

	dists, err := c.Reinstall.ListDistributions(testContext(t))
	if err != nil {
		t.Fatalf("ListDistributions: %v", err)
	}

	for _, d := range dists {
		oses, err := c.Reinstall.ListOperatingSystems(testContext(t), d.ID)
		if err != nil {
			continue
		}

		for _, o := range oses {
			if o.ID != excludeOSID {
				return o.ID
			}
		}
	}

	return excludeOSID
}
