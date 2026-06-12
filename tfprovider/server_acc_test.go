package tfprovider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// ---------------------------------------------------------------------------
// Server Name
// ---------------------------------------------------------------------------

// TestAccServerName_basic sets a descriptive label on an existing server,
// updates it, then destroys (which resets the label to the server hostname).
// Requires GIGAHOST_TEST_SERVER_ID.
func TestAccServerName_basic(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	name1 := accRandName("srv")
	name2 := name1 + "-v2"
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			// After destroy, the label should have been reset to the hostname.
			// We can't know the hostname here, but we can verify it's no
			// longer set to either of the names we applied.
			srv, err := client.Servers.Get(accCtx, serverID)
			if err != nil {
				return fmt.Errorf("CheckDestroy: get server: %w", err)
			}

			if srv.Label == name1 || srv.Label == name2 {
				return fmt.Errorf("server label %q still matches applied name after destroy", srv.Label)
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccServerNameConfig(serverID, name1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_name.test", "server_id", serverID),
					resource.TestCheckResourceAttr("gigahost_server_name.test", "name", name1),
					resource.TestCheckResourceAttrSet("gigahost_server_name.test", "id"),
				),
			},
			{
				Config: testAccServerNameConfig(serverID, name2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_name.test", "name", name2),
				),
			},
			{
				ResourceName:      "gigahost_server_name.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccServerNameConfig(serverID, name string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_name" "test" {
  server_id = %q
  name      = %q
}
`, testAccProviderConfig(), serverID, name)
}

// ---------------------------------------------------------------------------
// Server Reverse DNS
// ---------------------------------------------------------------------------

// TestAccServerRDNS_IPv4 sets, updates, and destroys (clears) the rDNS
// entry for an IPv4 address attached to an existing server.
// Requires GIGAHOST_TEST_SERVER_ID and GIGAHOST_TEST_SERVER_IP_ID.
func TestAccServerRDNS_IPv4(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP_ID")
	dns1 := "acc-test1.example.com"
	dns2 := "acc-test2.example.com"
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			// After destroy the reverse should be cleared (empty string).
			srv, err := client.Servers.Get(accCtx, serverID)
			if err != nil {
				return fmt.Errorf("CheckDestroy: get server: %w", err)
			}

			for _, ip := range srv.IPs {
				if ip.ID == ipID {
					if ip.Reverse == dns1 || ip.Reverse == dns2 {
						return fmt.Errorf("rDNS %q still set after destroy", ip.Reverse)
					}

					return nil
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccServerRDNSConfig(serverID, ipID, dns1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "server_id", serverID),
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "ip_id", ipID),
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "dns", dns1),
					resource.TestCheckResourceAttrSet("gigahost_server_rdns.test", "id"),
				),
			},
			{
				Config: testAccServerRDNSConfig(serverID, ipID, dns2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "dns", dns2),
				),
			},
			{
				ResourceName:      "gigahost_server_rdns.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(_ *terraform.State) (string, error) {
					return serverID + "/" + ipID, nil
				},
			},
		},
	})
}

func testAccServerRDNSConfig(serverID, ipID, dns string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_rdns" "test" {
  server_id = %q
  ip_id     = %q
  dns       = %q
}
`, testAccProviderConfig(), serverID, ipID, dns)
}

// ---------------------------------------------------------------------------
// Server Name + Reverse combined
// ---------------------------------------------------------------------------

// TestAccServerNameAndReverse applies both gigahost_server_name and
// gigahost_server_rdns to the same server in a single config to confirm
// they can coexist.
func TestAccServerNameAndReverse(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP_ID")
	name := accRandName("srv-combined")
	dns := "combined-acc-test.example.com"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccServerNameAndReverseConfig(serverID, ipID, name, dns),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_name.test", "name", name),
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "dns", dns),
				),
			},
		},
	})
}

func testAccServerNameAndReverseConfig(serverID, ipID, name, dns string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_server_name" "test" {
  server_id = %q
  name      = %q
}

resource "gigahost_server_rdns" "test" {
  server_id = %q
  ip_id     = %q
  dns       = %q
}
`, testAccProviderConfig(), serverID, name, serverID, ipID, dns)
}

// ---------------------------------------------------------------------------
// Server with DNS record
// ---------------------------------------------------------------------------

// TestAccServerWithDNSRecord creates a DNS A record pointing at a known IP
// that is associated with an existing server, then verifies the record is
// present in the zone.
//
// Requires GIGAHOST_TEST_SERVER_ID and GIGAHOST_TEST_ZONE_APEX.
// The IP to use must be supplied via GIGAHOST_TEST_SERVER_IP.
func TestAccServerWithDNSRecord(t *testing.T) {
	testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	serverIP := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP")
	zoneName := accZoneName(t, "srv-dns")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccServerWithDNSRecordConfig(zoneName, serverIP),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.srv", "type", "A"),
					resource.TestCheckResourceAttr("gigahost_dns_record.srv", "value", serverIP),
				),
			},
		},
	})
}

func testAccServerWithDNSRecordConfig(zoneName, ip string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "srv" {
  zone_id = gigahost_dns_zone.test.id
  type    = "A"
  value   = %q
  ttl     = 60
}
`, testAccProviderConfig(), zoneName, ip)
}

// testAccRequireEnv is declared in account_acc_test.go.
var _ = os.Getenv // ensure os is used
