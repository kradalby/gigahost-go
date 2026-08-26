package tfprovider_test

// OOB import tests for BGP-family resources. See import_oob_acc_test.go for the
// harness, shared design notes, and the full env-var matrix.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// BGP ASN (gated: a pre-approved ASN on the account)
// ---------------------------------------------------------------------------

// TestAccImportOOB_BGPASN imports an already-approved ASN by its bare ASN value
// and converges. ASN submission needs manual approval, so this never creates an
// ASN with the CLI — it imports the existing approved record named by
// GIGAHOST_TEST_ASN. Destroy is a no-op (an approved ASN cannot be withdrawn
// programmatically), so there is no CheckDestroy. Requires GIGAHOST_TOKEN and
// GIGAHOST_TEST_ASN.
func TestAccImportOOB_BGPASN(t *testing.T) {
	asn := testAccRequireEnv(t, "GIGAHOST_TEST_ASN")
	requireToken(t)

	bare := normalizeASN(asn)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBBGPASNConfig(bare)),
			{
				Config:             testAccOOBBGPASNConfig(bare),
				ResourceName:       "gigahost_bgp_asn.test",
				ImportState:        true,
				ImportStateId:      bare, // bare ASN
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("asn", bare),
				),
			},
			{
				Config: testAccOOBBGPASNConfig(bare),
				Check: resource.TestCheckResourceAttr(
					"gigahost_bgp_asn.test", "asn", bare,
				),
			},
		},
	})
}

// TestAccImportOOB_BGPASNPrefixed is the sibling of TestAccImportOOB_BGPASN
// importing by the AS-prefixed form ("AS"+number) to prove the import ID
// normalizes to the bare value in state. Requires the same env vars.
func TestAccImportOOB_BGPASNPrefixed(t *testing.T) {
	asn := testAccRequireEnv(t, "GIGAHOST_TEST_ASN")
	requireToken(t)

	bare := normalizeASN(asn)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBBGPASNConfig(bare)),
			{
				Config:             testAccOOBBGPASNConfig(bare),
				ResourceName:       "gigahost_bgp_asn.test",
				ImportState:        true,
				ImportStateId:      "AS" + bare, // AS-prefixed form
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					// Import ID was AS-prefixed; state must hold the bare value.
					checkImportAttr("asn", bare),
				),
			},
			{
				Config: testAccOOBBGPASNConfig(bare),
				Check: resource.TestCheckResourceAttr(
					"gigahost_bgp_asn.test", "asn", bare,
				),
			},
		},
	})
}

func testAccOOBBGPASNConfig(asn string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_bgp_asn" "test" {
  asn = %q
}
`, testAccOOBProviderConfig(), asn)
}

// normalizeASN strips an optional AS prefix and surrounding space, mirroring the
// provider's asnType normalization, so the config asn matches the state asn.
func normalizeASN(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.EqualFold(s[:2], "AS") {
		s = s[2:]
	}

	return s
}

// ---------------------------------------------------------------------------
// BGP session (gated: approved ASN ID + an IPv4 IP ID)
// ---------------------------------------------------------------------------

// TestAccImportOOB_BGPSession creates a BGP session with the CLI, imports
// gigahost_bgp_session by its session ID, and converges; destroy deletes the
// session and CheckDestroy verifies it is gone via BGP.Get. Requires
// GIGAHOST_TOKEN, GIGAHOST_TEST_ASN_ID (internal ASN record ID) and
// GIGAHOST_TEST_IPV4_IP_ID (an IPv4 address ID to peer with).
func TestAccImportOOB_BGPSession(t *testing.T) {
	asnID := testAccRequireEnv(t, "GIGAHOST_TEST_ASN_ID")
	ipID := testAccRequireEnv(t, "GIGAHOST_TEST_IPV4_IP_ID")
	requireToken(t)

	client := testAccGigahostClient(t)

	var created struct {
		ID           string
		ASNID        string
		IPID         string
		IPAddress    string
		Status       string
		DefaultRoute bool
	}

	runCLIJSON(
		t, &created,
		"bgp", "session", "create",
		"--asn-id", asnID,
		"--ipv4-id", ipID,
	)

	if created.ID == "" {
		t.Fatalf("CLI returned empty BGP session ID (asn-id=%s ipv4-id=%s)", asnID, ipID)
	}

	// Best-effort cleanup (runs after Terraform destroy).
	t.Cleanup(func() { _ = client.BGP.DeleteSession(accCtx, created.ID) })

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckOOBBGPSessionDestroyed(client, created.ID),
		Steps: []resource.TestStep{
			oobBootstrapStep(testAccOOBBGPSessionConfig(asnID, ipID)),
			{
				Config:             testAccOOBBGPSessionConfig(asnID, ipID),
				ResourceName:       "gigahost_bgp_session.test",
				ImportState:        true,
				ImportStateId:      created.ID,
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateChecks(
					checkImportAttr("id", created.ID),
					checkImportAttr("asn_id", asnID),
					checkImportAttr("ipv4_ip_id", ipID),
				),
			},
			{
				Config: testAccOOBBGPSessionConfig(asnID, ipID),
				Check: resource.TestCheckResourceAttr(
					"gigahost_bgp_session.test", "asn_id", asnID,
				),
			},
		},
	})
}

func testAccOOBBGPSessionConfig(asnID, ipID string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_bgp_session" "test" {
  asn_id     = %q
  ipv4_ip_id = %q

  # Read populates default_route=false after import; set it explicitly so the
  # follow-up plan is empty (it is Optional + non-Computed + RequiresReplace,
  # so a null config against a known false state would otherwise plan a replace).
  default_route = false
}
`, testAccOOBProviderConfig(), asnID, ipID)
}

func testAccCheckOOBBGPSessionDestroyed(client *gigahost.Client, sessionID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		data, err := client.BGP.Get(accCtx)
		if err != nil {
			return fmt.Errorf("CheckDestroy: get BGP data: %w", err)
		}

		for _, s := range data.Sessions {
			if s.ID == sessionID {
				return fmt.Errorf("BGP session %s still exists after destroy", sessionID)
			}
		}

		return nil
	}
}
