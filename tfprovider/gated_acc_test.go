package tfprovider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The resources in this file have live preconditions the standard test
// account cannot satisfy (registered .no domain, IP prefix allocation,
// approved ASN). Each test gates on an env var naming the prerequisite
// and skips cleanly when it is unset.

// TestAccDNSDNSSEC toggles DNSSEC on a registered domain hosted on
// Gigahost nameservers. GIGAHOST_TEST_REGISTERED_ZONE must hold the
// zone ID of a registered .no domain.
func TestAccDNSDNSSEC(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_REGISTERED_ZONE")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_dnssec" "test" {
  zone_id = %q
  enabled = true
}
`, testAccProviderConfig(), zoneID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_dnssec.test", "enabled", "true"),
					resource.TestCheckResourceAttrSet("gigahost_dns_dnssec.test", "ds_records"),
				),
			},
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_dnssec" "test" {
  zone_id = %q
  enabled = false
}
`, testAccProviderConfig(), zoneID),
				Check: resource.TestCheckResourceAttr("gigahost_dns_dnssec.test", "enabled", "false"),
			},
			{
				ResourceName:      "gigahost_dns_dnssec.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccDNSNameservers delegates a registered domain to external
// nameservers and reverts to the Gigahost defaults on destroy. The
// nameservers must already serve the zone (the API verifies before
// pushing to Norid).
func TestAccDNSNameservers(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_REGISTERED_ZONE")
	ns1 := testAccRequireEnv(t, "GIGAHOST_TEST_NS1")
	ns2 := testAccRequireEnv(t, "GIGAHOST_TEST_NS2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_nameservers" "test" {
  zone_id     = %q
  nameservers = [%q, %q]
}
`, testAccProviderConfig(), zoneID, ns1, ns2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_nameservers.test", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("gigahost_dns_nameservers.test", "nameservers.0", ns1),
				),
			},
			{
				// The API has no GET for nameservers, so import cannot recover the
				// list. The first apply after import will re-push the configured
				// nameservers.
				ResourceName:            "gigahost_dns_nameservers.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"nameservers"},
			},
		},
	})
}

// TestAccDNSExternalDSRecords submits DS records for a registered
// domain hosted on external DNSSEC-signed nameservers.
// GIGAHOST_TEST_EXTERNAL_ZONE is the zone ID; key tag/digest come from
// GIGAHOST_TEST_DS_KEY_TAG and GIGAHOST_TEST_DS_DIGEST (algorithm 13,
// digest type 2 assumed — SHA-256 ECDSA P-256, the common case).
func TestAccDNSExternalDSRecords(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_EXTERNAL_ZONE")
	keyTag := testAccRequireEnv(t, "GIGAHOST_TEST_DS_KEY_TAG")
	digest := testAccRequireEnv(t, "GIGAHOST_TEST_DS_DIGEST")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_external_ds_records" "test" {
  zone_id = %q

  ds_records = [{
    key_tag     = %s
    algorithm   = 13
    digest_type = 2
    digest      = %q
  }]
}
`, testAccProviderConfig(), zoneID, keyTag, digest),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_external_ds_records.test", "ds_records.#", "1"),
					resource.TestCheckResourceAttr("gigahost_dns_external_ds_records.test", "ds_records.0.digest", digest),
				),
			},
			{
				ResourceName:      "gigahost_dns_external_ds_records.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccDNSPTRZone creates and destroys a reverse-DNS zone for an IP
// prefix allocated to the account. GIGAHOST_TEST_IP_PREFIX holds the
// prefix (e.g. "185.125.168.0/24") and GIGAHOST_TEST_PTR_ZONE_NAME the
// matching in-addr.arpa zone name.
func TestAccDNSPTRZone(t *testing.T) {
	prefix := testAccRequireEnv(t, "GIGAHOST_TEST_IP_PREFIX")
	zoneName := testAccRequireEnv(t, "GIGAHOST_TEST_PTR_ZONE_NAME")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_dns_ptr_zone" "test" {
  prefix     = %q
  ip_version = "ipv4"
  zone_name  = %q
}
`, testAccProviderConfig(), prefix, zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_dns_ptr_zone.test", "id"),
					resource.TestCheckResourceAttr("gigahost_dns_ptr_zone.test", "zone_name", zoneName),
				),
			},
			{
				ResourceName:      "gigahost_dns_ptr_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccBGPASN submits an ASN for peering approval. Submission cannot
// be withdrawn programmatically (destroy is a no-op), so this only runs
// when GIGAHOST_TEST_ASN names an ASN you actually want submitted.
func TestAccBGPASN(t *testing.T) {
	asn := testAccRequireEnv(t, "GIGAHOST_TEST_ASN")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_bgp_asn" "test" {
  asn = %q
}
`, testAccProviderConfig(), asn),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_bgp_asn.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_bgp_asn.test", "status"),
				),
			},
			{
				// Import by ASN number, not by the internal record id, because
				// the API identifies records by ASN value in the /bgp listing.
				ResourceName:      "gigahost_bgp_asn.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["gigahost_bgp_asn.test"]
					if !ok {
						return "", errors.New("resource not found in state")
					}

					return rs.Primary.Attributes["asn"], nil
				},
			},
		},
	})
}

// TestAccBGPSession creates a BGP session for an already-approved ASN.
// GIGAHOST_TEST_ASN_ID is the internal ASN record ID and
// GIGAHOST_TEST_IPV4_IP_ID an IPv4 address ID to peer with.
func TestAccBGPSession(t *testing.T) {
	asnID := testAccRequireEnv(t, "GIGAHOST_TEST_ASN_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_IPV4_IP_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_bgp_session" "test" {
  asn_id     = %q
  ipv4_ip_id = %q
}
`, testAccProviderConfig(), asnID, ipID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_bgp_session.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_bgp_session.test", "status"),
				),
			},
			{
				ResourceName:      "gigahost_bgp_session.test",
				ImportState:       true,
				ImportStateVerify: true,
				// redundant materialises server-side as extra sessions;
				// it cannot be determined from a single session record.
				ImportStateVerifyIgnore: []string{"redundant"},
			},
		},
	})
}

// TestAccServerIPv4 orders an additional IPv4 onto an existing server.
//
// This BILLS a real IP that the API CANNOT release (see upstream B18), so it is
// gated on GIGAHOST_TEST_SERVER_ID naming a server you accept ordering an IP
// onto. deletion_policy = "retain" means destroy only drops it from state; the
// address stays allocated until released in the control panel. Import verifies
// the address round-trips; type/deletion_policy are config-only (the API does
// not report them) and are ignored on import.
func TestAccServerIPv4(t *testing.T) {
	serverID := testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_ID")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
%s

resource "gigahost_server_ipv4" "test" {
  server_id       = %q
  type            = "l3"
  deletion_policy = "retain"
}
`, testAccProviderConfig(), serverID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_server_ipv4.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_server_ipv4.test", "address"),
					resource.TestCheckResourceAttr("gigahost_server_ipv4.test", "server_id", serverID),
					resource.TestCheckResourceAttr("gigahost_server_ipv4.test", "version", "4"),
				),
			},
			{
				ResourceName:      "gigahost_server_ipv4.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["gigahost_server_ipv4.test"]
					if !ok {
						return "", errors.New("resource not found in state")
					}

					return rs.Primary.Attributes["server_id"] + "/" + rs.Primary.Attributes["id"], nil
				},
				// type and deletion_policy are config-only; the API does not
				// report them, so import cannot recover them.
				ImportStateVerifyIgnore: []string{"type", "deletion_policy"},
			},
		},
	})
}
