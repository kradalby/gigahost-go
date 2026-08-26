package tfprovider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestAccServerSnapshot deploys a server, snapshots it, verifies the snapshot
// appears, then destroys everything (snapshot deleted, server cancelled).
func TestAccServerSnapshot(t *testing.T) {
	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osSlug := accPickOS(t, client)
	pub, _ := accEphemeralKey(t)
	sshName := accRandName("snap-ssh")
	snapName := accRandName("snap")

	var serverID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckServerCancelled(client, &serverID),
		Steps: []resource.TestStep{
			{
				Config: testAccServerSnapshotConfig(sshName, pub, typeSlug, sizeSlug, osSlug, snapName),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureAttr("gigahost_server.test", "id", &serverID),
					resource.TestCheckResourceAttr("gigahost_server_snapshot.test", "name", snapName),
					resource.TestCheckResourceAttrSet("gigahost_server_snapshot.test", "snapshot_id"),
					resource.TestCheckResourceAttrSet("gigahost_server_snapshot.test", "state"),
					testAccCheckSnapshotExists(client, &serverID, snapName),
				),
			},
			{
				ResourceName:      "gigahost_server_snapshot.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["gigahost_server_snapshot.test"]
					if !ok {
						return "", errors.New("resource gigahost_server_snapshot.test not found in state")
					}

					sID := rs.Primary.Attributes["server_id"]
					snapID := rs.Primary.Attributes["snapshot_id"]

					return sID + "/" + snapID, nil
				},
			},
		},
	})
}

func testAccServerSnapshotConfig(sshName, pubKey, typeSlug, sizeSlug, osSlug, snapName string) string {
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
  ssh_keys = [gigahost_account_ssh_key.test.id]
}

resource "gigahost_server_snapshot" "test" {
  server_id = gigahost_server.test.id
  name      = %q
}
`, testAccProviderConfig(), sshName, pubKey, typeSlug, sizeSlug, osSlug, snapName)
}

// testAccCheckSnapshotExists verifies the snapshot is present on the server,
// matched by its display name.
func testAccCheckSnapshotExists(client *gigahost.Client, serverID *string, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		snaps, err := client.Snapshots.List(accCtx, *serverID)
		if err != nil {
			return fmt.Errorf("list snapshots: %w", err)
		}

		for _, s := range snaps {
			if s.DisplayName == name {
				return nil
			}
		}

		return fmt.Errorf("snapshot %q not found on server %s", name, *serverID)
	}
}
