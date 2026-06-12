package tfprovider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccServerCatalogDataSource reads the full deploy catalog. Read-only and
// free; decode must succeed and the catalog must expose tiers, products with a
// category, and regions.
func TestAccServerCatalogDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_server_catalog" "all" {}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_server_catalog.all", "currency"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_catalog.all", "tiers.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_catalog.all", "regions.#"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_catalog.all", "tiers.0.products.0.product_id"),
					resource.TestCheckResourceAttrSet("data.gigahost_server_catalog.all", "tiers.0.products.0.price_id"),
					resource.TestMatchResourceAttr("data.gigahost_server_catalog.all", "tiers.0.products.0.category",
						regexp.MustCompile(`^(vm|dedicated|auction)$`)),
				),
			},
		},
	})
}

// TestAccServerSizeDataSourceCategory resolves a cloud size and checks the new
// category attribute is populated. Read-only and free.
func TestAccServerSizeDataSourceCategory(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_server_size" "small" {
  memory_gb = 4
  cheapest  = true
}
`, testAccProviderConfig()),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_server_size.small", "slug"),
					resource.TestMatchResourceAttr("data.gigahost_server_size.small", "category",
						regexp.MustCompile(`^(vm|dedicated|auction)$`)),
				),
			},
		},
	})
}

// TestAccSSHKeysDataSource lists account SSH keys; decode must succeed even
// when the account has none. Read-only and free.
func TestAccSSHKeysDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s
data "gigahost_ssh_keys" "all" {}
`, testAccProviderConfig()),
				Check: resource.TestCheckResourceAttrSet("data.gigahost_ssh_keys.all", "keys.#"),
			},
		},
	})
}

// TestAccDNSRecordsDataSource creates a zone with a record and reads every
// record back through the data source. Free (no billing), but needs a DNS apex
// to create the zone — skipped via accZoneName when GIGAHOST_TEST_ZONE_APEX is
// unset.
func TestAccDNSRecordsDataSource(t *testing.T) {
	zoneName := accZoneName(t, "ds-records")

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

resource "gigahost_dns_record" "a" {
  zone_id = gigahost_dns_zone.test.id
  name    = "www"
  type    = "A"
  value   = "192.0.2.1"
  ttl     = 3600
}

data "gigahost_dns_records" "all" {
  zone       = gigahost_dns_zone.test.id
  depends_on = [gigahost_dns_record.a]
}
`, testAccProviderConfig(), zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gigahost_dns_records.all", "records.#"),
					// Match on type+value to avoid brittleness over relative vs FQDN names.
					resource.TestCheckTypeSetElemNestedAttrs("data.gigahost_dns_records.all", "records.*", map[string]string{
						"type":  "A",
						"value": "192.0.2.1",
					}),
				),
			},
		},
	})
}
