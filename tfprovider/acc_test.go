package tfprovider_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/tfprovider"
)

// TestMain sets the provider source env vars terraform-plugin-testing needs to
// drive OpenTofu. Without them every acc test fails at `init` with an empty
// ("-") provider namespace. Setting them here means a plain
// `TF_ACC=1 go test ./tfprovider/...` (against OpenTofu) works.
func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC_PROVIDER_NAMESPACE") == "" {
		os.Setenv("TF_ACC_PROVIDER_NAMESPACE", "hashicorp")
	}

	if os.Getenv("TF_ACC_PROVIDER_HOST") == "" {
		os.Setenv("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
	}

	// resource.TestMain runs registered sweepers when `-sweep` is passed and
	// otherwise behaves like m.Run().
	resource.TestMain(m)
}

// testAccProviderFactories is the provider factory map shared by all acceptance
// tests. The provider is served in-process via Protocol 6.
var testAccProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"gigahost": providerserver.NewProtocol6WithError(tfprovider.New("test")()),
}

// testAccPreCheck aborts the test immediately if the mandatory token is absent.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("GIGAHOST_TOKEN") == "" {
		t.Fatal("GIGAHOST_TOKEN must be set for acceptance tests")
	}
}

// testAccPreCheckAPIKeys skips API-key acceptance tests when the token cannot
// manage API keys. The typical API-key token is 403 on /account/apikeys, which
// is a token-scope limitation rather than a provider defect.
func testAccPreCheckAPIKeys(t *testing.T) {
	t.Helper()

	testAccPreCheck(t)

	c := testAccGigahostClient(t)

	_, err := c.Account.ListAPIKeys(accCtx)
	if err == nil {
		return
	}

	var apiErr *gigahost.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		t.Skip("token lacks permission to manage API keys; skipping")
	}
}

// testAccProviderConfig returns a minimal provider block; the token is picked
// up automatically from the GIGAHOST_TOKEN environment variable.
func testAccProviderConfig() string {
	return `provider "gigahost" {}`
}

// accRandName returns a collision-resistant resource name for a test run.
func accRandName(prefix string) string {
	return fmt.Sprintf("tf-acc-%s-%06d", prefix, rand.Intn(1_000_000))
}

// accZoneName returns a unique DNS zone name beneath the apex domain supplied
// by GIGAHOST_TEST_ZONE_APEX. The test is skipped if the variable is unset.
func accZoneName(t *testing.T, prefix string) string {
	t.Helper()

	apex := os.Getenv("GIGAHOST_TEST_ZONE_APEX")
	if apex == "" {
		t.Skip("GIGAHOST_TEST_ZONE_APEX must be set for DNS acceptance tests")
	}

	return fmt.Sprintf("%s-%06d.%s", prefix, rand.Intn(1_000_000), apex)
}

// testAccGigahostClient builds a live client from GIGAHOST_TOKEN, used inside
// CheckDestroy functions to verify cleanup against the real API.
func testAccGigahostClient(t *testing.T) *gigahost.Client {
	t.Helper()

	token := os.Getenv("GIGAHOST_TOKEN")
	if token == "" {
		t.Fatal("GIGAHOST_TOKEN not set")
	}

	c, err := gigahost.NewClient(gigahost.WithToken(token))
	if err != nil {
		t.Fatalf("testAccGigahostClient: %v", err)
	}

	return c
}

// accCtx is the context used in CheckDestroy closures where no *testing.T is
// available.
var accCtx = context.Background()
