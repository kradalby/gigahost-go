package tfprovider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// ---------------------------------------------------------------------------
// SSH key tests
// ---------------------------------------------------------------------------

// TestAccSSHKey_basic creates an SSH key, verifies computed attributes are
// populated, exercises ImportState, then destroys and confirms the key is gone
// from the live API.
func TestAccSSHKey_basic(t *testing.T) {
	name := accRandName("sshkey")
	// The API validates SSH keys, so use a freshly generated, valid one.
	pubKey, _ := accEphemeralKey(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			client := testAccGigahostClient(t)

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
		},
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyConfig(name, pubKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_ssh_key.test", "name", name),
					resource.TestCheckResourceAttrSet("gigahost_account_ssh_key.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_account_ssh_key.test", "added_at"),
				),
			},
			{
				// public_key is now populated by Read; no longer ignored.
				ResourceName:      "gigahost_account_ssh_key.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccSSHKeyConfig(name, pubKey string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}
`, testAccProviderConfig(), name, pubKey)
}

// ---------------------------------------------------------------------------
// API key tests
// ---------------------------------------------------------------------------

// TestAccAPIKey_basic creates an API key with minimal config, checks that the
// secret is set in state after creation (it's never returned again), exercises
// ImportState, then destroys.
func TestAccAPIKey_basic(t *testing.T) {
	label := accRandName("apikey")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckAPIKeys(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			client := testAccGigahostClient(t)

			keys, err := client.Account.ListAPIKeys(accCtx)
			if err != nil {
				return fmt.Errorf("CheckDestroy: list API keys: %w", err)
			}

			for _, k := range keys {
				if k.Label == label {
					return fmt.Errorf("API key %q still exists after destroy", label)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfig(label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "label", label),
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "secret"),
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "prefix"),
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "status", "active"),
				),
			},
			{
				// The secret is write-once; ignore it during import verification.
				ResourceName:            "gigahost_account_api_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
		},
	})
}

func testAccAPIKeyConfig(label string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_api_key" "test" {
  label = %q
}
`, testAccProviderConfig(), label)
}

// TestAccAPIKey_updateLabel verifies that an API key label can be updated
// in-place without recreating the key (the ID must remain the same).
func TestAccAPIKey_updateLabel(t *testing.T) {
	label1 := accRandName("apikey")
	label2 := label1 + "-updated"

	var keyID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckAPIKeys(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			client := testAccGigahostClient(t)

			keys, err := client.Account.ListAPIKeys(accCtx)
			if err != nil {
				return fmt.Errorf("CheckDestroy: list API keys: %w", err)
			}

			for _, k := range keys {
				if k.Label == label1 || k.Label == label2 {
					return fmt.Errorf("API key still exists after destroy (label %q)", k.Label)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfig(label1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "label", label1),
					testAccExtractAttr("gigahost_account_api_key.test", "id", &keyID),
				),
			},
			{
				Config: testAccAPIKeyConfig(label2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "label", label2),
					// Verify the key was updated, not replaced.
					testAccCheckAttrEquals("gigahost_account_api_key.test", "id", &keyID),
				),
			},
		},
	})
}

// TestAccAPIKey_withPermissions creates a key scoped to DNS read-write access
// across all zones, then updates to add server read access.
func TestAccAPIKey_withPermissions(t *testing.T) {
	label := accRandName("apikey-perms")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckAPIKeys(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			client := testAccGigahostClient(t)

			keys, err := client.Account.ListAPIKeys(accCtx)
			if err != nil {
				return fmt.Errorf("CheckDestroy: list API keys: %w", err)
			}

			for _, k := range keys {
				if k.Label == label {
					return fmt.Errorf("API key %q still exists after destroy", label)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfigWithDNSPerm(label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "label", label),
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "permissions.dns.mode", "rw"),
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "permissions.dns.all", "true"),
				),
			},
			{
				// The secret is write-once; ignore it during import verification.
				ResourceName:            "gigahost_account_api_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
			{
				Config: testAccAPIKeyConfigWithDNSAndServerPerm(label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "permissions.dns.mode", "rw"),
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "permissions.servers.mode", "r"),
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "permissions.servers.all", "true"),
				),
			},
		},
	})
}

func testAccAPIKeyConfigWithDNSPerm(label string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_api_key" "test" {
  label = %q

  permissions = {
    dns = {
      mode = "rw"
      all  = true
    }
  }
}
`, testAccProviderConfig(), label)
}

func testAccAPIKeyConfigWithDNSAndServerPerm(label string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_api_key" "test" {
  label = %q

  permissions = {
    dns = {
      mode = "rw"
      all  = true
    }
    servers = {
      mode = "r"
      all  = true
    }
  }
}
`, testAccProviderConfig(), label)
}

// TestAccAPIKey_withExpiry creates a key with an expiry timestamp. Because
// changing expires_at forces resource replacement, we verify that a second
// step with a different expiry yields a new ID.
func TestAccAPIKey_withExpiry(t *testing.T) {
	label := accRandName("apikey-expiry")

	// Far-future expiry so the key remains usable during the test.
	expiry1 := int64(9_000_000_000) // ~2255
	expiry2 := int64(9_100_000_000)

	var id1 string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckAPIKeys(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			client := testAccGigahostClient(t)

			keys, err := client.Account.ListAPIKeys(accCtx)
			if err != nil {
				return fmt.Errorf("CheckDestroy: list API keys: %w", err)
			}

			for _, k := range keys {
				if k.Label == label {
					return fmt.Errorf("API key %q still exists after destroy", label)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKeyConfigWithExpiry(label, expiry1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_account_api_key.test", "label", label),
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "expires_at"),
					testAccExtractAttr("gigahost_account_api_key.test", "id", &id1),
				),
			},
			{
				// Changing expires_at must force a new resource (different ID).
				Config: testAccAPIKeyConfigWithExpiry(label, expiry2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "id"),
					testAccCheckAttrNotEquals("gigahost_account_api_key.test", "id", &id1),
				),
			},
		},
	})
}

func testAccAPIKeyConfigWithExpiry(label string, expiresAt int64) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_api_key" "test" {
  label      = %q
  expires_at = %d
}
`, testAccProviderConfig(), label, expiresAt)
}

// TestAccSSHKeyAndAPIKey creates both resource types together and verifies the
// account data source is readable.
func TestAccSSHKeyAndAPIKey(t *testing.T) {
	sshName := accRandName("sshkey")
	apiLabel := accRandName("apikey")

	pubKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCx9lw5YqG2GQmFpMc9" +
		"1q9V0q1lU7g3hA7Y3sV2MNc6EqL9h8yHklPd4J5yXkV8Q3hKzGcXuqXkK" +
		"zNmR7VfJ terraform-acc-test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckAPIKeys(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyAndAPIKeyConfig(sshName, pubKey, apiLabel),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gigahost_account_ssh_key.test", "id"),
					resource.TestCheckResourceAttrSet("gigahost_account_api_key.test", "secret"),
					resource.TestCheckResourceAttrSet("data.gigahost_account.me", "id"),
				),
			},
		},
	})
}

func testAccSSHKeyAndAPIKeyConfig(sshName, pubKey, apiLabel string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}

resource "gigahost_account_api_key" "test" {
  label = %q
}

data "gigahost_account" "me" {}
`, testAccProviderConfig(), sshName, pubKey, apiLabel)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testAccExtractAttr captures an attribute value from state into dst for later
// comparison steps.
func testAccExtractAttr(resourceName, attr string, dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %q not found in state", resourceName)
		}

		v, ok := rs.Primary.Attributes[attr]
		if !ok {
			return fmt.Errorf("attribute %q not found on %s", attr, resourceName)
		}

		*dst = v

		return nil
	}
}

// testAccCheckAttrEquals fails if the named attribute does not equal *expected.
func testAccCheckAttrEquals(resourceName, attr string, expected *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %q not found in state", resourceName)
		}

		got := rs.Primary.Attributes[attr]
		if got != *expected {
			return fmt.Errorf("%s.%s = %q, want %q", resourceName, attr, got, *expected)
		}

		return nil
	}
}

// testAccCheckAttrNotEquals fails if the named attribute equals *notExpected.
func testAccCheckAttrNotEquals(resourceName, attr string, notExpected *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %q not found in state", resourceName)
		}

		got := rs.Primary.Attributes[attr]
		if got == *notExpected {
			return fmt.Errorf("%s.%s = %q, expected a different value (resource should have been replaced)",
				resourceName, attr, got)
		}

		return nil
	}
}

// testAccRequireEnv skips the test if the named environment variable is unset.
func testAccRequireEnv(t *testing.T, key string) string {
	t.Helper()

	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s must be set for this acceptance test", key)
	}

	return v
}
