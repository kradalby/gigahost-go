package tfprovider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccServerIPv4_deploy deploys a throwaway cheapest-cloud server, orders an
// additional l3 IPv4 onto it, and verifies the address attached. It is fully
// self-cleaning: destroy drops the IP from state (deletion_policy = retain) and
// cancels the server, which frees the ordered IP along with it. This is the
// end-to-end proof that gigahost_server_ipv4 attaches a real IP to a real
// server. It bills the server for a few minutes (cheapest VPS) like the other
// deploy acceptance tests.
func TestAccServerIPv4_deploy(t *testing.T) {
	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osSlug := accPickOS(t, client)
	pub, _ := accEphemeralKey(t)
	sshName := accRandName("ipv4-ssh")

	var serverID string

	config := fmt.Sprintf(`
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

resource "gigahost_server_ipv4" "extra" {
  server_id       = gigahost_server.test.id
  type            = "l3"
  deletion_policy = "retain"
}
`, testAccProviderConfig(), sshName, pub, typeSlug, sizeSlug, osSlug)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckServerCancelled(client, &serverID),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					captureAttr("gigahost_server.test", "id", &serverID),
					resource.TestCheckResourceAttrSet("gigahost_server_ipv4.extra", "id"),
					resource.TestCheckResourceAttrSet("gigahost_server_ipv4.extra", "address"),
					resource.TestCheckResourceAttr("gigahost_server_ipv4.extra", "version", "4"),
					resource.TestCheckResourceAttrPair(
						"gigahost_server_ipv4.extra", "server_id", "gigahost_server.test", "id",
					),
					// The server resource exposes its nested IP list.
					resource.TestCheckResourceAttrSet("gigahost_server.test", "ips.#"),
				),
			},
		},
	})
}
