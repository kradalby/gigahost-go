package tfprovider_test

// OOB import tests for server-family resources. See import_oob_acc_test.go for
// the harness, shared design notes, and the full env-var matrix.

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// Server name (gated: existing server)
// ---------------------------------------------------------------------------

// TestAccImportOOB_ServerName renames an existing server with the CLI, imports
// gigahost_server_name by the server ID, and converges. Requires GIGAHOST_TOKEN
// and GIGAHOST_TEST_SERVER_ID.
//
// Destroy semantics: the resource's Delete reverts the label to the server's
// hostname (it does not delete the server). There is therefore no CheckDestroy
// demanding server deletion; instead t.Cleanup restores the original label that
// was captured before the test, so the server is left exactly as found.
func TestAccImportOOB_ServerName(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	requireToken(t)

	client := testAccGigahostClient(t)

	// Capture the original label so cleanup can restore it.
	original, err := client.Servers.Get(accCtx, serverID)
	if err != nil {
		t.Fatalf("get server %s: %v", serverID, err)
	}

	originalLabel := original.Label
	name := accRandName("oob-srvname")

	runCLIJSON(t, nil, "servers", "rename", "--name", name, serverID)

	t.Cleanup(func() { _ = client.Servers.UpdateName(accCtx, serverID, originalLabel) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBServerNameConfig(serverID, name)),
			{
				Config:             testAccOOBServerNameConfig(serverID, name),
				ResourceName:       "gigahost_server_name.test",
				ImportState:        true,
				ImportStateId:      serverID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("server_id", serverID),
					checkImportAttr("name", name),
				),
			},
			{
				Config: testAccOOBServerNameConfig(serverID, name),
				Check: resource.TestCheckResourceAttr(
					"gigahost_server_name.test", "name", name,
				),
			},
		},
	})
}

func testAccOOBServerNameConfig(serverID, name string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_name" "test" {
  server_id = %q
  name      = %q
}
`, testAccOOBProviderConfig(), serverID, name)
}

// ---------------------------------------------------------------------------
// Server reverse DNS (gated: existing server + IPv4 IP ID)
// ---------------------------------------------------------------------------

// TestAccImportOOB_ServerRDNS sets a unique rDNS value on a server IP with the
// CLI, imports gigahost_server_rdns by "<serverID>/<ipID>", and converges.
// Destroy clears the rDNS (the resource's Delete pushes an empty reverse), and
// CheckDestroy verifies it is cleared. Requires GIGAHOST_TOKEN,
// GIGAHOST_TEST_SERVER_ID and GIGAHOST_TEST_SERVER_IP_ID.
func TestAccImportOOB_ServerRDNS(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP_ID")
	requireToken(t)

	client := testAccGigahostClient(t)
	dns := accRandName("oob-rdns") + ".example.com"

	runCLIJSON(t, nil, "servers", "reverse", "--ip-id", ipID, "--dns", dns, serverID)

	// Best-effort: clear the reverse regardless of exit path.
	t.Cleanup(func() {
		_ = client.Servers.UpdateReverse(accCtx, serverID, gigahost.UpdateReverseRequest{
			IPID: ipID,
			DNS:  "",
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBRDNSCleared(client, serverID, ipID, dns),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBServerRDNSConfig(serverID, ipID, dns)),
			{
				Config:             testAccOOBServerRDNSConfig(serverID, ipID, dns),
				ResourceName:       "gigahost_server_rdns.test",
				ImportState:        true,
				ImportStateId:      serverID + "/" + ipID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("server_id", serverID),
					checkImportAttr("ip_id", ipID),
					checkImportAttr("dns", dns),
				),
			},
			{
				Config: testAccOOBServerRDNSConfig(serverID, ipID, dns),
				Check: resource.TestCheckResourceAttr(
					"gigahost_server_rdns.test", "dns", dns,
				),
			},
		},
	})
}

func testAccOOBServerRDNSConfig(serverID, ipID, dns string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_rdns" "test" {
  server_id = %q
  ip_id     = %q
  dns       = %q
}
`, testAccOOBProviderConfig(), serverID, ipID, dns)
}

func testAccCheckOOBRDNSCleared(client *gigahost.Client, serverID, ipID, dns string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		srv, err := client.Servers.Get(accCtx, serverID)
		if err != nil {
			return fmt.Errorf("CheckDestroy: get server: %w", err)
		}

		for _, ip := range srv.IPs {
			if ip.ID == ipID {
				if ip.Reverse == dns {
					return fmt.Errorf("rDNS %q still set on ip %s after destroy", dns, ipID)
				}

				return nil
			}
		}

		return nil
	}
}

// ---------------------------------------------------------------------------
// Server snapshot (gated: existing server)
// ---------------------------------------------------------------------------

// TestAccImportOOB_ServerSnapshot creates a snapshot of an existing server with
// the CLI, waits (bounded) for it to complete, imports
// gigahost_server_snapshot by "<serverID>/<snapshotID>", and converges; destroy
// deletes the snapshot and CheckDestroy verifies it is gone. Requires
// GIGAHOST_TOKEN and GIGAHOST_TEST_SERVER_ID.
func TestAccImportOOB_ServerSnapshot(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	requireToken(t)

	client := testAccGigahostClient(t)
	snapName := accRandName("oob-snap")

	var created struct {
		ID          int64
		DisplayName string
		State       string
	}

	runCLIJSON(t, &created, "servers", "snapshots", "create", "--name", snapName, serverID)

	if created.ID == 0 {
		t.Fatalf("CLI returned empty snapshot ID for %q", snapName)
	}

	snapID := strconv.FormatInt(created.ID, 10)

	// Best-effort cleanup of the snapshot (runs after Terraform destroy).
	t.Cleanup(func() { _ = client.Snapshots.Delete(accCtx, serverID, created.ID) })

	// Snapshot create is async; bound the wait for it to reach "completed".
	waitForSnapshotCompleted(t, client, serverID, created.ID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBSnapshotDestroyed(client, serverID, created.ID),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBSnapshotConfig(serverID, snapName)),
			{
				Config:             testAccOOBSnapshotConfig(serverID, snapName),
				ResourceName:       "gigahost_server_snapshot.test",
				ImportState:        true,
				ImportStateId:      serverID + "/" + snapID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("server_id", serverID),
					checkImportAttr("snapshot_id", snapID),
					checkImportAttr("name", snapName),
				),
			},
			{
				Config: testAccOOBSnapshotConfig(serverID, snapName),
				Check: resource.TestCheckResourceAttr(
					"gigahost_server_snapshot.test", "name", snapName,
				),
			},
		},
	})
}

func testAccOOBSnapshotConfig(serverID, name string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_snapshot" "test" {
  server_id = %q
  name      = %q
}
`, testAccOOBProviderConfig(), serverID, name)
}

// waitForSnapshotCompleted polls until the snapshot reaches the completed state
// or a bound elapses (then it fails — a never-completing snapshot is a real
// problem, not something to skip over). The bound mirrors the resource's own
// 10-minute snapshotAppearTimeout.
func waitForSnapshotCompleted(t *testing.T, client *gigahost.Client, serverID string, snapID int64) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Minute)

	for time.Now().Before(deadline) {
		snaps, err := client.Snapshots.List(accCtx, serverID)
		if err != nil {
			t.Fatalf("list snapshots: %v", err)
		}

		for _, s := range snaps {
			if s.ID == snapID && s.State == gigahost.SnapshotStateCompleted {
				return
			}
		}

		time.Sleep(10 * time.Second)
	}

	t.Fatalf("snapshot %d on server %s did not complete within 10m", snapID, serverID)
}

func testAccCheckOOBSnapshotDestroyed(client *gigahost.Client, serverID string, snapID int64) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		snaps, err := client.Snapshots.List(accCtx, serverID)
		if err != nil {
			return fmt.Errorf("CheckDestroy: list snapshots: %w", err)
		}

		for _, s := range snaps {
			if s.ID == snapID {
				return fmt.Errorf("snapshot %d still exists on server %s after destroy", snapID, serverID)
			}
		}

		return nil
	}
}

// ---------------------------------------------------------------------------
// Server (gated: REAL MONEY — deploys and cancels a server)
// ---------------------------------------------------------------------------

// TestAccImportOOB_Server deploys the cheapest cloud server with the CLI,
// imports gigahost_server by the server ID, applies a matching config, then
// destroys (cancels) the server. The follow-up apply exercises the only live
// coverage of the adoptImported Update branch (importing leaves type/size null
// in state, so the apply enters adoption rather than a normal update).
//
// COST WARNING: this deploys a real, hourly-billed server and cancels it at the
// end. It is gated behind GIGAHOST_TEST_OOB_DEPLOY=1 in addition to
// GIGAHOST_TOKEN so it never runs by accident. The type/size/os are driven from
// the live catalog (cheapest product, Debian preferred) — never hardcoded.
func TestAccImportOOB_Server(t *testing.T) {
	if os.Getenv("GIGAHOST_TEST_OOB_DEPLOY") != "1" {
		t.Skip("GIGAHOST_TEST_OOB_DEPLOY=1 required (this test deploys a real, billed server)")
	}

	requireToken(t)

	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osSlug := accPickOS(t, client)

	var deployed []struct {
		ServerID string
		Hostname string
		IP       string
		Password string
		OrderID  string
	}

	runCLIJSON(
		t, &deployed,
		"deploy", "create",
		"--type", typeSlug,
		"--size", sizeSlug,
		"--os", osSlug,
		"--wait",
	)

	if len(deployed) == 0 || deployed[0].ServerID == "" {
		t.Fatalf("deploy returned no ready server: %+v", deployed)
	}

	serverID := deployed[0].ServerID

	// Belt-and-braces: cancel the server even if the framework destroy fails.
	t.Cleanup(func() { _ = client.Servers.Cancel(accCtx, serverID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBServerCancelled(client, serverID),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBServerConfig(typeSlug, sizeSlug, osSlug)),
			{
				Config:             testAccOOBServerConfig(typeSlug, sizeSlug, osSlug),
				ResourceName:       "gigahost_server.test",
				ImportState:        true,
				ImportStateId:      serverID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("id", serverID),
					checkImportAttr("os", osSlug),
				),
			},
			{
				// Same config: type/size are null in the imported state, so this
				// apply enters adoptImported — the sole live exercise of it.
				Config: testAccOOBServerConfig(typeSlug, sizeSlug, osSlug),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server.test", "type", typeSlug),
					resource.TestCheckResourceAttr("gigahost_server.test", "size", sizeSlug),
					// Computed deploy facts must be filled from the catalog during
					// adoption; if they stay unknown the apply fails with
					// "provider produced inconsistent result".
					resource.TestCheckResourceAttrSet("gigahost_server.test", "memory_gb"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "storage_gb"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "rate_hourly"),
					resource.TestCheckResourceAttrSet("gigahost_server.test", "rate_monthly"),
				),
			},
		},
	})
}

func testAccOOBServerConfig(typeSlug, sizeSlug, osSlug string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server" "test" {
  type = %q
  size = %q
  os   = %q
}
`, testAccOOBProviderConfig(), typeSlug, sizeSlug, osSlug)
}

// testAccCheckOOBServerCancelled verifies, after destroy, that the server was
// cancelled: a repeat cancel must fail because the order is already gone.
func testAccCheckOOBServerCancelled(client *gigahost.Client, serverID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if err := client.Servers.Cancel(accCtx, serverID); err == nil {
			return fmt.Errorf("server %s was still cancellable after destroy; Delete may not have cancelled it", serverID)
		}

		return nil
	}
}
