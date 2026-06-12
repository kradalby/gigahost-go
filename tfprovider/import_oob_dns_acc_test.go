package tfprovider_test

// OOB import tests for DNS-family resources that live inside a zone or need a
// registered/delegated domain. See import_oob_acc_test.go for the harness, the
// shared design notes, and the full env-var matrix.

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// DNS record (inside a CLI-created zone)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSRecord creates a zone and an A record with the CLI,
// imports the record by "<zoneID>/<recordID>", and verifies convergence. The
// zone is deliberately kept OUT of Terraform — it is OOB context — so the
// config holds only the gigahost_dns_record, referencing the zone by its ID
// string. Requires GIGAHOST_TOKEN and GIGAHOST_TEST_ZONE_APEX.
func TestAccImportOOB_DNSRecord(t *testing.T) {
	requireToken(t)

	zoneName := accZoneName(t, "oob-rec")
	client := testAccGigahostClient(t)

	var zone struct {
		ID   string
		Name string
		Type string
	}

	runCLIJSON(t, &zone, "dns", "zones", "create", zoneName, "--type", "NATIVE")

	if zone.ID == "" {
		t.Fatalf("CLI returned empty zone ID for %q", zoneName)
	}

	// The zone is OOB context; remove it once Terraform has released the record.
	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, zone.ID) })

	const (
		recName  = "oob"
		recType  = "A"
		recValue = "192.0.2.10"
		recTTL   = 3600
	)

	var record struct {
		ID       string
		Name     string
		Type     string
		Value    string
		TTL      int
		Priority string
	}

	runCLIJSON(
		t, &record,
		"dns", "records", "create",
		"--zone", zone.ID,
		"--name", recName,
		"--type", recType,
		"--value", recValue,
		"--ttl", strconv.Itoa(recTTL),
	)

	if record.ID == "" {
		t.Fatalf("CLI returned empty record ID for %s/%s", zoneName, recName)
	}

	importID := zone.ID + "/" + record.ID

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBRecordDestroyed(client, zone.ID, record.ID),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBDNSRecordConfig(zone.ID, recName, recType, recValue, recTTL)),
			{
				Config:             testAccOOBDNSRecordConfig(zone.ID, recName, recType, recValue, recTTL),
				ResourceName:       "gigahost_dns_record.test",
				ImportState:        true,
				ImportStateId:      importID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("name", recName),
					checkImportAttr("type", recType),
					checkImportAttr("value", recValue),
				),
			},
			{
				Config: testAccOOBDNSRecordConfig(zone.ID, recName, recType, recValue, recTTL),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_record.test", "value", recValue,
				),
			},
		},
	})
}

func testAccOOBDNSRecordConfig(zoneID, name, recType, value string, ttl int) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_record" "test" {
  zone_id = %q
  name    = %q
  type    = %q
  value   = %q
  ttl     = %d
}
`, testAccOOBProviderConfig(), zoneID, name, recType, value, ttl)
}

// testAccCheckOOBRecordDestroyed verifies the record is gone from its (still
// existing) zone after Terraform destroy.
func testAccCheckOOBRecordDestroyed(client *gigahost.Client, zoneID, recordID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		records, err := client.DNS.ListRecords(accCtx, zoneID)
		if err != nil {
			// Zone may already be gone via cleanup ordering; treat as destroyed.
			return nil //nolint:nilerr // a missing zone implies the record is gone
		}

		for _, r := range records {
			if r.ID == recordID {
				return fmt.Errorf("record %s still exists in zone %s after destroy", recordID, zoneID)
			}
		}

		return nil
	}
}

// ---------------------------------------------------------------------------
// DNS redirect (inside a CLI-created zone)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSRedirect creates a zone and an HTTP redirect with the
// CLI, imports the redirect by "<zoneID>/<source>", and verifies convergence.
// The zone is OOB context (kept out of Terraform). Requires GIGAHOST_TOKEN and
// GIGAHOST_TEST_ZONE_APEX.
func TestAccImportOOB_DNSRedirect(t *testing.T) {
	requireToken(t)

	zoneName := accZoneName(t, "oob-redir")
	client := testAccGigahostClient(t)

	var zone struct {
		ID   string
		Name string
		Type string
	}

	runCLIJSON(t, &zone, "dns", "zones", "create", zoneName, "--type", "NATIVE")

	if zone.ID == "" {
		t.Fatalf("CLI returned empty zone ID for %q", zoneName)
	}

	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, zone.ID) })

	const (
		source    = "go"
		targetURL = "https://example.com"
	)

	// The redirect create row has no ID field; source is the import key.
	runCLIJSON(
		t, nil,
		"dns", "redirects", "create",
		"--zone", zone.ID,
		"--source", source,
		"--target-url", targetURL,
	)

	importID := zone.ID + "/" + source

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBRedirectDestroyed(client, zone.ID, source),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBDNSRedirectConfig(zone.ID, source, targetURL)),
			{
				Config:             testAccOOBDNSRedirectConfig(zone.ID, source, targetURL),
				ResourceName:       "gigahost_dns_redirect.test",
				ImportState:        true,
				ImportStateId:      importID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("source", source),
					checkImportAttr("target_url", targetURL),
				),
			},
			{
				Config: testAccOOBDNSRedirectConfig(zone.ID, source, targetURL),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_redirect.test", "target_url", targetURL,
				),
			},
		},
	})
}

func testAccOOBDNSRedirectConfig(zoneID, source, targetURL string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_redirect" "test" {
  zone_id    = %q
  source     = %q
  target_url = %q
}
`, testAccOOBProviderConfig(), zoneID, source, targetURL)
}

func testAccCheckOOBRedirectDestroyed(client *gigahost.Client, zoneID, source string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		redirects, err := client.DNS.ListRedirects(accCtx, zoneID)
		if err != nil {
			return nil //nolint:nilerr // a missing zone implies the redirect is gone
		}

		for _, r := range redirects {
			if r.Source == source {
				return fmt.Errorf("redirect %q still exists in zone %s after destroy", source, zoneID)
			}
		}

		return nil
	}
}

// ---------------------------------------------------------------------------
// DNSSEC (gated: registered domain on Gigahost nameservers)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSSEC enables DNSSEC on a registered domain with the CLI,
// imports the resource by zone ID, and verifies convergence (enabled=true).
// Destroy disables DNSSEC. Requires GIGAHOST_TOKEN and
// GIGAHOST_TEST_REGISTERED_ZONE (zone ID of a registered .no domain). Mirrors
// TestAccDNSDNSSEC in gated_acc_test.go: DNSSEC can only be enabled for
// registered domains, otherwise the API returns 403.
func TestAccImportOOB_DNSSEC(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_REGISTERED_ZONE")
	requireToken(t)

	client := testAccGigahostClient(t)

	// Enable DNSSEC out-of-band; on enable the CLI renders DS records, which we
	// do not need (we import by the known zone ID).
	runCLIJSON(t, nil, "dns", "dnssec", "enable", zoneID)

	// Best-effort: leave DNSSEC disabled regardless of how the test exits.
	t.Cleanup(func() { _ = client.DNS.SetDNSSEC(accCtx, zoneID, false) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBDNSSECConfig(zoneID)),
			{
				Config:             testAccOOBDNSSECConfig(zoneID),
				ResourceName:       "gigahost_dns_dnssec.test",
				ImportState:        true,
				ImportStateId:      zoneID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("zone_id", zoneID),
					checkImportAttr("enabled", "true"),
				),
			},
			{
				Config: testAccOOBDNSSECConfig(zoneID),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_dnssec.test", "enabled", "true",
				),
			},
		},
	})
}

func testAccOOBDNSSECConfig(zoneID string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_dnssec" "test" {
  zone_id = %q
  enabled = true
}
`, testAccOOBProviderConfig(), zoneID)
}

// ---------------------------------------------------------------------------
// DNS nameservers (gated: registered domain + two delegated nameservers)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSNameservers delegates a registered domain to external
// nameservers with the CLI, imports by zone ID, and converges. The nameservers
// API has no GET, so the resource's Read is a no-op: after import the
// nameservers attribute is null in state. The single follow-up Config step
// therefore plans a change (null -> [ns1, ns2]) and re-pushes the nameservers
// during apply; the framework's post-apply plan then re-reads the (unchanged,
// because Read is a no-op) state against the same config and finds it empty —
// so one follow-up step is sufficient to prove convergence. Destroy reverts to
// the Gigahost default nameservers.
//
// Requires GIGAHOST_TOKEN, GIGAHOST_TEST_REGISTERED_ZONE, GIGAHOST_TEST_NS1 and
// GIGAHOST_TEST_NS2. The nameservers must already serve the zone (the API
// verifies before pushing to Norid).
func TestAccImportOOB_DNSNameservers(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_REGISTERED_ZONE")
	ns1 := testAccRequireEnv(t, "GIGAHOST_TEST_NS1")
	ns2 := testAccRequireEnv(t, "GIGAHOST_TEST_NS2")
	requireToken(t)

	client := testAccGigahostClient(t)

	runCLIJSON(
		t, nil,
		"dns", "nameservers", "set",
		"--nameserver", ns1,
		"--nameserver", ns2,
		zoneID,
	)

	// Best-effort: revert to the Gigahost defaults regardless of exit path.
	t.Cleanup(func() {
		_ = client.DNS.SetNameservers(accCtx, zoneID, []string{"ns1.gigahost.no", "ns2.gigahost.no"})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBNameserversConfig(zoneID, ns1, ns2)),
			{
				Config:             testAccOOBNameserversConfig(zoneID, ns1, ns2),
				ResourceName:       "gigahost_dns_nameservers.test",
				ImportState:        true,
				ImportStateId:      zoneID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("zone_id", zoneID),
				),
			},
			{
				// Re-pushes the nameservers (null -> list) then plans empty.
				Config: testAccOOBNameserversConfig(zoneID, ns1, ns2),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_nameservers.test", "nameservers.0", ns1,
				),
			},
		},
	})
}

func testAccOOBNameserversConfig(zoneID, ns1, ns2 string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_nameservers" "test" {
  zone_id     = %q
  nameservers = [%q, %q]
}
`, testAccOOBProviderConfig(), zoneID, ns1, ns2)
}

// ---------------------------------------------------------------------------
// DNS external DS records (gated: registered domain on external DNSSEC NS)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSExternalDS submits an external DS record for a registered
// domain with the CLI, imports by zone ID, and converges. Requires
// GIGAHOST_TOKEN, GIGAHOST_TEST_EXTERNAL_ZONE, GIGAHOST_TEST_DS_KEY_TAG and
// GIGAHOST_TEST_DS_DIGEST (algorithm 13, digest type 2 assumed — the common
// SHA-256 ECDSA P-256 case, matching TestAccDNSExternalDSRecords).
func TestAccImportOOB_DNSExternalDS(t *testing.T) {
	zoneID := testAccRequireEnv(t, "GIGAHOST_TEST_EXTERNAL_ZONE")
	keyTag := testAccRequireEnv(t, "GIGAHOST_TEST_DS_KEY_TAG")
	digest := testAccRequireEnv(t, "GIGAHOST_TEST_DS_DIGEST")
	requireToken(t)

	runCLIJSON(
		t, nil,
		"dns", "dnssec", "submit-external",
		"--key-tag", keyTag,
		"--algorithm", "13",
		"--digest-type", "2",
		"--digest", digest,
		zoneID,
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBExternalDSConfig(zoneID, keyTag, digest)),
			{
				Config:             testAccOOBExternalDSConfig(zoneID, keyTag, digest),
				ResourceName:       "gigahost_dns_external_ds_records.test",
				ImportState:        true,
				ImportStateId:      zoneID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("zone_id", zoneID),
					checkImportAttr("ds_records.0.digest", digest),
				),
			},
			{
				Config: testAccOOBExternalDSConfig(zoneID, keyTag, digest),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_external_ds_records.test", "ds_records.0.digest", digest,
				),
			},
		},
	})
}

func testAccOOBExternalDSConfig(zoneID, keyTag, digest string) string {
	return fmt.Sprintf(`
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
`, testAccOOBProviderConfig(), zoneID, keyTag, digest)
}

// ---------------------------------------------------------------------------
// DNS PTR zone (gated: allocated IP prefix)
// ---------------------------------------------------------------------------

// TestAccImportOOB_DNSPTRZone creates a reverse-DNS (PTR) zone with the CLI,
// imports it by zone ID, and converges; destroy deletes the PTR zone. Requires
// GIGAHOST_TOKEN, GIGAHOST_TEST_IP_PREFIX (e.g. "185.125.168") and
// GIGAHOST_TEST_PTR_ZONE_NAME (the matching in-addr.arpa zone name).
func TestAccImportOOB_DNSPTRZone(t *testing.T) {
	prefix := testAccRequireEnv(t, "GIGAHOST_TEST_IP_PREFIX")
	zoneName := testAccRequireEnv(t, "GIGAHOST_TEST_PTR_ZONE_NAME")
	requireToken(t)

	client := testAccGigahostClient(t)

	var created struct {
		ZoneID   string
		ZoneName string
		Prefix   string
		Version  string
	}

	runCLIJSON(
		t, &created,
		"dns", "ptr", "create",
		"--prefix", prefix,
		"--version", "ipv4",
		"--zone-name", zoneName,
	)

	if created.ZoneID == "" {
		t.Fatalf("CLI returned empty PTR zone ID for %q", zoneName)
	}

	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, created.ZoneID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBPTRZoneConfig(prefix, zoneName)),
			{
				Config:             testAccOOBPTRZoneConfig(prefix, zoneName),
				ResourceName:       "gigahost_dns_ptr_zone.test",
				ImportState:        true,
				ImportStateId:      created.ZoneID, // by opaque zone ID
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("zone_name", zoneName),
					checkImportAttr("ip_version", "ipv4"),
				),
			},
			{
				Config: testAccOOBPTRZoneConfig(prefix, zoneName),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_ptr_zone.test", "zone_name", zoneName,
				),
			},
		},
	})
}

// TestAccImportOOB_DNSPTRZoneByName is the sibling of TestAccImportOOB_DNSPTRZone
// importing by the arpa zone NAME instead of the opaque zone ID. Requires the
// same env vars.
func TestAccImportOOB_DNSPTRZoneByName(t *testing.T) {
	prefix := testAccRequireEnv(t, "GIGAHOST_TEST_IP_PREFIX")
	zoneName := testAccRequireEnv(t, "GIGAHOST_TEST_PTR_ZONE_NAME")
	requireToken(t)

	client := testAccGigahostClient(t)

	var created struct {
		ZoneID   string
		ZoneName string
		Prefix   string
		Version  string
	}

	runCLIJSON(
		t, &created,
		"dns", "ptr", "create",
		"--prefix", prefix,
		"--version", "ipv4",
		"--zone-name", zoneName,
	)

	if created.ZoneID == "" {
		t.Fatalf("CLI returned empty PTR zone ID for %q", zoneName)
	}

	t.Cleanup(func() { _ = client.DNS.DeleteZone(accCtx, created.ZoneID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckDNSZoneDestroyed(client, zoneName),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBPTRZoneConfig(prefix, zoneName)),
			{
				Config:             testAccOOBPTRZoneConfig(prefix, zoneName),
				ResourceName:       "gigahost_dns_ptr_zone.test",
				ImportState:        true,
				ImportStateId:      zoneName, // by arpa zone name
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("zone_name", zoneName),
				),
			},
			{
				Config: testAccOOBPTRZoneConfig(prefix, zoneName),
				Check: resource.TestCheckResourceAttr(
					"gigahost_dns_ptr_zone.test", "zone_name", zoneName,
				),
			},
		},
	})
}

func testAccOOBPTRZoneConfig(prefix, zoneName string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_dns_ptr_zone" "test" {
  prefix     = %q
  ip_version = "ipv4"
  zone_name  = %q
}
`, testAccOOBProviderConfig(), prefix, zoneName)
}
