package tfprovider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDNSZoneDataSource creates a zone and reads it back through the
// gigahost_dns_zone data source, by name and by ID.
func TestAccDNSZoneDataSource(t *testing.T) {
	zoneName := accZoneName(t, "ds-zone")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
}

data "gigahost_dns_zone" "by_name" {
  name = gigahost_dns_zone.test.name
}

data "gigahost_dns_zone" "by_id" {
  id = gigahost_dns_zone.test.id
}
`, testAccProviderConfig(), zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.gigahost_dns_zone.by_name", "name", zoneName),
					resource.TestCheckResourceAttrPair("data.gigahost_dns_zone.by_name", "id", "gigahost_dns_zone.test", "id"),
					resource.TestCheckResourceAttr("data.gigahost_dns_zone.by_id", "name", zoneName),
				),
			},
		},
	})
}

// TestAccDNSZonesDataSource lists all zones and expects the freshly created
// zone to make the list non-empty.
func TestAccDNSZonesDataSource(t *testing.T) {
	zoneName := accZoneName(t, "ds-zones")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
}

data "gigahost_dns_zones" "all" {
  depends_on = [gigahost_dns_zone.test]
}
`, testAccProviderConfig(), zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_dns_zones.all", "zones.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_dns_zones.all", "zones.0.id"),
				),
			},
		},
	})
}

// TestAccServersDataSource lists all servers; decode must succeed even when
// the account has none.
func TestAccServersDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_servers" "all" {}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_servers.all", "servers.#"),
				),
			},
		},
	})
}

// TestAccBGPDataSource reads the BGP overview; decode must succeed even with
// no ASNs or sessions registered.
func TestAccBGPDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_bgp" "all" {}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_bgp.all", "asns.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_bgp.all", "sessions.#"),
				),
			},
		},
	})
}
