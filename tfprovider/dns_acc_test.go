package tfprovider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// DNS Zone
// ---------------------------------------------------------------------------

// TestAccDNSZone_basic creates a NATIVE DNS zone, verifies computed attributes,
// exercises import, then destroys and confirms the zone is gone via the API.
func TestAccDNSZone_basic(t *testing.T) {
	zoneName := accZoneName(t, "zone")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSZoneConfig(zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_zone.test", "name", zoneName),
					resource.TestCheckResourceAttrSet("gigahost_dns_zone.test", "id"),
					resource.TestCheckResourceAttr("gigahost_dns_zone.test", "type", "NATIVE"),
					resource.TestCheckResourceAttr("gigahost_dns_zone.test", "active", "true"),
				),
			},
			// Import by opaque zone ID (the default).
			{
				ResourceName:      "gigahost_dns_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Import by zone name — the friendly-identifier path.
			{
				ResourceName:      "gigahost_dns_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(_ *terraform.State) (string, error) {
					return zoneName, nil
				},
			},
		},
	})
}

func testAccDNSZoneConfig(zoneName string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}
`, testAccProviderConfig(), zoneName)
}

// ---------------------------------------------------------------------------
// DNS Records
// ---------------------------------------------------------------------------

// TestAccDNSRecord_A creates a zone with an A record, updates the value, then
// destroys everything.
func TestAccDNSRecord_A(t *testing.T) {
	zoneName := accZoneName(t, "rec-a")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordConfig(zoneName, "A", "@", "1.2.3.4", 3600, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "A"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "1.2.3.4"),
					resource.TestCheckResourceAttrSet("gigahost_dns_record.test", "record_id"),
				),
			},
			{
				// Update the record value in-place.
				Config: testAccDNSRecordConfig(zoneName, "A", "@", "5.6.7.8", 3600, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "5.6.7.8"),
				),
			},
			{
				ResourceName:      "gigahost_dns_record.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Import by zone name + record ID — the friendly-identifier path.
			{
				ResourceName:      "gigahost_dns_record.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["gigahost_dns_record.test"]
					if !ok {
						return "", errors.New("gigahost_dns_record.test not found in state")
					}

					recordID := rs.Primary.Attributes["record_id"]

					return zoneName + "/" + recordID, nil
				},
			},
		},
	})
}

// TestAccDNSRecord_AAAA creates an AAAA record.
func TestAccDNSRecord_AAAA(t *testing.T) {
	zoneName := accZoneName(t, "rec-aaaa")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordConfig(zoneName, "AAAA", "@", "2001:db8::1", 3600, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "AAAA"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "2001:db8::1"),
				),
			},
		},
	})
}

// TestAccDNSRecord_CNAME creates a CNAME record.
func TestAccDNSRecord_CNAME(t *testing.T) {
	zoneName := accZoneName(t, "rec-cname")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				// Use "alias" (not "www") to avoid colliding with the zone's
				// default www A record, and store the value without a trailing dot
				// to match the API's normalisation.
				Config: testAccDNSRecordConfig(zoneName, "CNAME", "alias", "example.com", 3600, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "CNAME"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "name", "alias"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "example.com"),
				),
			},
		},
	})
}

// TestAccDNSRecord_MX creates an MX record with priority, then updates the priority.
func TestAccDNSRecord_MX(t *testing.T) {
	zoneName := accZoneName(t, "rec-mx")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordMXConfig(zoneName, "mail.example.com", 10),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "MX"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "priority", "10"),
				),
			},
			{
				Config: testAccDNSRecordMXConfig(zoneName, "mail.example.com", 20),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "priority", "20"),
				),
			},
		},
	})
}

// TestAccDNSRecord_TXT creates a TXT record.
func TestAccDNSRecord_TXT(t *testing.T) {
	zoneName := accZoneName(t, "rec-txt")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordConfig(zoneName, "TXT", "@", "v=spf1 -all", 3600, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "type", "TXT"),
					resource.TestCheckResourceAttr("gigahost_dns_record.test", "value", "v=spf1 -all"),
				),
			},
		},
	})
}

// TestAccDNSRecord_multipleInZone creates a zone with three different record
// types and verifies they coexist without conflict.
func TestAccDNSRecord_multipleInZone(t *testing.T) {
	zoneName := accZoneName(t, "rec-multi")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSMultipleRecordsConfig(zoneName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_record.a", "type", "A"),
					resource.TestCheckResourceAttr("gigahost_dns_record.aaaa", "type", "AAAA"),
					resource.TestCheckResourceAttr("gigahost_dns_record.txt", "type", "TXT"),
				),
			},
		},
	})
}

// ---------------------------------------------------------------------------
// DNS Redirect
// ---------------------------------------------------------------------------

// TestAccDNSRedirect_basic creates an HTTP redirect inside a zone, updates the
// target URL, exercises import, then destroys.
func TestAccDNSRedirect_basic(t *testing.T) {
	zoneName := accZoneName(t, "redirect")
	client := testAccGigahostClient(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRedirectConfig(zoneName, "www", "https://example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_redirect.test", "source", "www"),
					resource.TestCheckResourceAttr("gigahost_dns_redirect.test", "target_url", "https://example.com"),
					resource.TestCheckResourceAttr("gigahost_dns_redirect.test", "enabled", "true"),
					resource.TestCheckResourceAttrSet("gigahost_dns_redirect.test", "id"),
				),
			},
			{
				// Update the target URL in-place.
				Config: testAccDNSRedirectConfig(zoneName, "www", "https://example.org"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_dns_redirect.test", "target_url", "https://example.org"),
				),
			},
			{
				ResourceName:      "gigahost_dns_redirect.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Import by zone name + source — the friendly-identifier path.
			{
				ResourceName:      "gigahost_dns_redirect.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(_ *terraform.State) (string, error) {
					return zoneName + "/www", nil
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// testAccDNSRecordConfig builds HCL for a zone + one record. Pass an empty
// string for nameAttr when the record name should be omitted (defaulting to @).
// The extra string is appended verbatim and can carry additional attributes
// such as priority.
func testAccDNSRecordConfig(zoneName, recType, name, value string, ttl int, extra string) string {
	nameAttr := ""
	if name != "" && name != "@" {
		nameAttr = fmt.Sprintf("name  = %q", name)
	}

	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "test" {
  zone_id = gigahost_dns_zone.test.id
  type    = %q
  value   = %q
  ttl     = %d
  %s
  %s
}
`, testAccProviderConfig(), zoneName, recType, value, ttl, nameAttr, extra)
}

func testAccDNSRecordMXConfig(zoneName, value string, priority int) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "test" {
  zone_id  = gigahost_dns_zone.test.id
  type     = "MX"
  value    = %q
  ttl      = 3600
  priority = %d
}
`, testAccProviderConfig(), zoneName, value, priority)
}

func testAccDNSMultipleRecordsConfig(zoneName string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_record" "a" {
  zone_id = gigahost_dns_zone.test.id
  type    = "A"
  value   = "1.2.3.4"
  ttl     = 3600
}

resource "gigahost_dns_record" "aaaa" {
  zone_id = gigahost_dns_zone.test.id
  type    = "AAAA"
  value   = "2001:db8::1"
  ttl     = 3600
}

resource "gigahost_dns_record" "txt" {
  zone_id = gigahost_dns_zone.test.id
  name    = "info"
  type    = "TXT"
  value   = "hello from terraform acc test"
  ttl     = 3600
}
`, testAccProviderConfig(), zoneName)
}

func testAccDNSRedirectConfig(zoneName, source, targetURL string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_zone" "test" {
  name = %q
  type = "NATIVE"
}

resource "gigahost_dns_redirect" "test" {
  zone_id    = gigahost_dns_zone.test.id
  source     = %q
  target_url = %q
}
`, testAccProviderConfig(), zoneName, source, targetURL)
}

// ---------------------------------------------------------------------------
// Shared CheckDestroy helpers
// ---------------------------------------------------------------------------

// testAccCheckDNSZoneDestroyed returns a CheckDestroy function that verifies
// the given zone name no longer exists in the live API.
func testAccCheckDNSZoneDestroyed(client *gigahost.Client, zoneName string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		zones, err := client.DNS.ListZones(accCtx)
		if err != nil {
			return fmt.Errorf("CheckDestroy: list zones: %w", err)
		}

		for _, z := range zones {
			if z.Name == zoneName {
				return fmt.Errorf("DNS zone %q still exists after destroy", zoneName)
			}
		}

		return nil
	}
}
