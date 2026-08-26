package tfprovider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// Data source smoke tests
// ---------------------------------------------------------------------------

// TestAccAccountDataSource verifies that the account data source can be read
// and returns a non-empty ID (sanity check for token validity).
func TestAccAccountDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_account" "me" {}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_account.me", "id"),
				),
			},
		},
	})
}

// TestAccOperatingSystemsDataSource verifies the flat OS list decodes, the
// distribution filter narrows it, and the singular lookup resolves a slug.
func TestAccOperatingSystemsDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_operating_systems" "all" {}

data "gigahost_operating_systems" "debian" {
  distribution = "debian"
}

data "gigahost_operating_system" "debian" {
  distribution = "debian"
  release      = "12"
}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_operating_systems.all", "operating_systems.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_operating_systems.all", "operating_systems.0.slug"),
					resource.TestCheckResourceAttr("data.gigahost_operating_systems.debian", "operating_systems.0.distribution", "debian"),
					resource.TestCheckResourceAttrSet("data.gigahost_operating_systems.debian", "operating_systems.0.slug"),
					resource.TestCheckResourceAttr("data.gigahost_operating_system.debian", "slug", "debian-12"),
					resource.TestCheckResourceAttr("data.gigahost_operating_system.debian", "codename", "bookworm"),
				),
			},
		},
	})
}

// TestAccServerSizesDataSources verifies size/region discovery against the
// live catalog: the plural list is non-empty with grammar-valid slugs, the
// singular cheapest lookup resolves, and regions resolve by slug.
func TestAccServerSizesDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_server_sizes" "all" {}

data "gigahost_server_sizes" "cloud" {
  platform = "cloud"
}

data "gigahost_server_size" "cheapest" {
  cheapest = true
}

data "gigahost_regions" "all" {}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_server_sizes.all", "sizes.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_sizes.all", "sizes.0.slug"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_sizes.all", "sizes.0.type"),
					resource.TestCheckResourceAttr("data.gigahost_server_sizes.cloud", "sizes.0.platform", "cloud"),
					resource.TestMatchResourceAttr("data.gigahost_server_sizes.cloud", "sizes.0.slug",
						regexp.MustCompile(`^\d+c-\d+gb-\d+gb$`)),
					resource.TestMatchResourceAttr("data.gigahost_server_size.cheapest", "slug",
						regexp.MustCompile(`^\d+c-\d+gb-\d+gb$`)),
					resource.TestCheckResourceAttr("data.gigahost_server_size.cheapest", "platform", "cloud"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_size.cheapest", "type"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_size.cheapest", "rate_hourly"),
					resource.TestCheckResourceAttrSet("data.gigahost_regions.all", "regions.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_regions.all", "regions.0.slug"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// SSH key + DNS combined
// ---------------------------------------------------------------------------

// TestAccSSHAndDNS creates an SSH key and a DNS zone with an A record in a
// single plan. This exercises cross-resource ordering and ensures neither
// resource blocks the other.
func TestAccSSHAndDNS(t *testing.T) {
	sshName := accRandName("int-ssh")
	zoneName := accZoneName(t, "int-dns")
	pubKey, _ := accEphemeralKey(t)
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDNSZoneDestroyed(client, zoneName),
			testAccCheckSSHKeyNameDestroyed(client, sshName),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccSSHAndDNSConfig(sshName, pubKey, zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_account_ssh_key.test", "id"),
					resource.TestCheckResourceAttr("gigahost_dns_zone.test", "name", zoneName),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "A"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "192.0.2.1"),
				),
			},
			// Look the key back up by name through the data source, and
			// decode the (possibly empty) ISO list.
			{
				Config: testAccSSHAndDNSConfig(sshName, pubKey, zoneName) + fmt.Sprintf(`
data "gigahost_ssh_key" "by_name" {
  name       = %q
  depends_on = [gigahost_account_ssh_key.test]
}

data "gigahost_isos" "all" {}
`, sshName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.gigahost_ssh_key.by_name", "id",
						"gigahost_account_ssh_key.test", "id",
					),
					resource.TestCheckResourceAttrSet("data.gigahost_isos.all", "isos.#"),
				),
			},
		},
	})
}

func testAccSSHAndDNSConfig(sshName, pubKey, zoneName string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "test" {
  zone_id = gigahost_dns_zone.test.id
  type    = "A"
  value   = "192.0.2.1"
  ttl     = 60
}
`, testAccProviderConfig(), sshName, pubKey, zoneName)
}

// ---------------------------------------------------------------------------
// Full server integration
// ---------------------------------------------------------------------------

// TestAccFullServerLifecycle exercises the complete real-server scenario:
//
//  1. An existing server (GIGAHOST_TEST_SERVER_ID) is given a Terraform-managed
//     label, reverse DNS entry, and a DNS A record in a freshly created zone.
//  2. The server label and DNS record are then updated.
//  3. All managed resources are destroyed; the server itself remains (it cannot
//     be deleted via the API).
//
// Required env vars: GIGAHOST_TEST_SERVER_ID, GIGAHOST_TEST_SERVER_IP_ID,
// GIGAHOST_TEST_SERVER_IP, GIGAHOST_TEST_ZONE_APEX.
func TestAccFullServerLifecycle(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP_ID")
	serverIP := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_IP")
	zoneName := accZoneName(t, "int-srv")

	name1 := accRandName("srv-int")
	name2 := name1 + "-updated"
	dns := fmt.Sprintf("rdns-%s.example.com", name1)
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDNSZoneDestroyed(client, zoneName),
			testAccCheckServerNameReverted(client, serverID, name1, name2),
			testAccCheckServerRDNSCleared(client, serverID, ipID, dns),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccFullServerConfig(serverID, ipID, name1, dns, zoneName, serverIP),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_name.test", "name", name1),
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "dns", dns),
					resource.TestCheckResourceAttr("gigahost_dns_zone.test", "name", zoneName),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", serverIP),
				),
			},
			{
				// Update only the server name — reverse DNS and zone stay the same.
				Config: testAccFullServerConfig(serverID, ipID, name2, dns, zoneName, serverIP),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server_name.test", "name", name2),
					// reverse and zone are unchanged
					resource.TestCheckResourceAttr("gigahost_server_rdns.test", "dns", dns),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", serverIP),
				),
			},
		},
	})
}

func testAccFullServerConfig(serverID, ipID, name, dns, zoneName, ip string) string {
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

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "test" {
  zone_id = gigahost_dns_zone.test.id
  type    = "A"
  value   = %q
  ttl     = 60
}
`, testAccProviderConfig(), serverID, name, serverID, ipID, dns, zoneName, ip)
}

// ---------------------------------------------------------------------------
// CheckDestroy helpers
// ---------------------------------------------------------------------------

// testAccCheckSSHKeyNameDestroyed returns a TestCheckFunc that fails if an SSH
// key with the given name still exists on the account.
func testAccCheckSSHKeyNameDestroyed(client *gigahost.Client, name string) resource.TestCheckFunc {
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

// testAccCheckServerNameReverted fails if the server label still matches any of
// the applied names (implying the Delete logic failed to reset it).
func testAccCheckServerNameReverted(client *gigahost.Client, serverID string, names ...string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		srv, err := client.Servers.Get(accCtx, serverID)
		if err != nil {
			return fmt.Errorf("CheckDestroy: get server: %w", err)
		}

		for _, n := range names {
			if srv.Label == n {
				return fmt.Errorf("server label %q still matches applied name %q after destroy", srv.Label, n)
			}
		}

		return nil
	}
}

// testAccCheckServerRDNSCleared fails if the given IP still has the applied
// rDNS set (implying the Delete logic failed to clear it).
func testAccCheckServerRDNSCleared(client *gigahost.Client, serverID, ipID, dns string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		srv, err := client.Servers.Get(accCtx, serverID)
		if err != nil {
			return fmt.Errorf("CheckDestroy: get server: %w", err)
		}

		for _, ip := range srv.IPs {
			if ip.ID == ipID && ip.Reverse == dns {
				return fmt.Errorf("rDNS %q still set on IP %s after destroy", dns, ipID)
			}
		}

		return nil
	}
}

// Note: SSH key-login and DNS-resolution validation helpers land with the
// Go SDK e2e base in a follow-up (sshRun / sshKeyLogin), so the placeholder
// TCP-banner and net.LookupHost helpers were removed.
