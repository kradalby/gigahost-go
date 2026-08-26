package tfprovider_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// Sweepers remove resources left behind by interrupted acceptance tests. Run
// with `go test ./tfprovider/ -sweep=all`. Every test resource is named with
// the `tf-acc-` prefix (see accRandName), except DNS zones, which are
// subdomains of GIGAHOST_TEST_ZONE_APEX (see accZoneName).
const sweepPrefix = "tf-acc-"

func init() {
	resource.AddTestSweepers("gigahost_account_ssh_key", &resource.Sweeper{
		Name: "gigahost_account_ssh_key",
		F:    sweepSSHKeys,
	})
	resource.AddTestSweepers("gigahost_account_api_key", &resource.Sweeper{
		Name: "gigahost_account_api_key",
		F:    sweepAPIKeys,
	})
	resource.AddTestSweepers("gigahost_server", &resource.Sweeper{
		Name: "gigahost_server",
		F:    sweepServers,
	})
	resource.AddTestSweepers("gigahost_dns_zone", &resource.Sweeper{
		Name: "gigahost_dns_zone",
		F:    sweepDNSZones,
	})
}

// sweepClient builds a live client for sweepers, which have no *testing.T.
func sweepClient() (*gigahost.Client, error) {
	token := os.Getenv("GIGAHOST_TOKEN")
	if token == "" {
		return nil, errors.New("GIGAHOST_TOKEN must be set to sweep")
	}

	return gigahost.NewClient(gigahost.WithToken(token))
}

func sweepSSHKeys(string) error {
	c, err := sweepClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	account, err := c.Account.Get(ctx)
	if err != nil {
		return err
	}

	for i := range account.SSHKeys {
		k := account.SSHKeys[i]
		if !strings.HasPrefix(k.Name, sweepPrefix) {
			continue
		}

		if derr := c.Account.DeleteSSHKey(ctx, k.ID); derr != nil {
			return fmt.Errorf("delete ssh key %s: %w", k.ID, derr)
		}
	}

	return nil
}

func sweepAPIKeys(string) error {
	c, err := sweepClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	keys, err := c.Account.ListAPIKeys(ctx)
	if err != nil {
		// A token without the account scope cannot manage API keys; treat as
		// nothing to sweep rather than a sweep failure.
		var apiErr *gigahost.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
			return nil
		}

		return err
	}

	for i := range keys {
		k := keys[i]
		if !strings.HasPrefix(k.Label, sweepPrefix) {
			continue
		}

		if derr := c.Account.DeleteAPIKey(ctx, k.ID); derr != nil {
			return fmt.Errorf("delete api key %s: %w", k.ID, derr)
		}
	}

	return nil
}

func sweepServers(string) error {
	c, err := sweepClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	servers, err := c.Servers.List(ctx)
	if err != nil {
		return err
	}

	for i := range servers {
		s := servers[i]
		if !strings.HasPrefix(s.Label, sweepPrefix) && !strings.HasPrefix(s.Name, sweepPrefix) {
			continue
		}

		if derr := c.Servers.Cancel(ctx, s.ID); derr != nil {
			return fmt.Errorf("cancel server %s: %w", s.ID, derr)
		}
	}

	return nil
}

func sweepDNSZones(string) error {
	apex := os.Getenv("GIGAHOST_TEST_ZONE_APEX")
	if apex == "" {
		// Test zones are subdomains of the apex; without it there is nothing
		// safe to identify as test-created.
		return nil
	}

	c, err := sweepClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	zones, err := c.DNS.ListZones(ctx)
	if err != nil {
		return err
	}

	suffix := "." + apex

	for i := range zones {
		z := zones[i]
		// Suffix alone is not enough: the apex is a real domain, and every
		// other subzone under it belongs to somebody. Require the run prefix,
		// exactly as the ssh-key and api-key sweepers do.
		if z.Name == apex || !strings.HasSuffix(z.Name, suffix) || !strings.HasPrefix(z.Name, sweepPrefix) {
			continue
		}

		if derr := c.DNS.DeleteZone(ctx, z.ID); derr != nil {
			return fmt.Errorf("delete zone %s: %w", z.ID, derr)
		}
	}

	return nil
}
