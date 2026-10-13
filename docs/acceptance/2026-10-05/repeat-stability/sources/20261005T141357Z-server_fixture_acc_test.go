package tfprovider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccServerFixture supplies a disposable server to existing-server tests.
// The IDs stay inside these subtests, so unrelated account resources are never
// selected by the repeat runner.
func TestAccServerFixture(t *testing.T) {
	testAccRequireEnv(t, "GIGAHOST_TEST_SERVER_FIXTURE")
	client := testAccGigahostClient(t)
	client, _ = accSuiteAPI(t, client)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osSlug := accPickOS(t, client)
	pub, _ := accEphemeralKey(t)

	var serverID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckServerCancelled(client, &serverID),
		Steps: []resource.TestStep{{
			Config: testAccServerConfig(accRandName("fixture-ssh"), pub, typeSlug, sizeSlug, osSlug),
			Check: resource.ComposeAggregateTestCheckFunc(
				captureAttr("gigahost_server.test", "id", &serverID),
				resource.TestCheckResourceAttr("gigahost_server.test", "status", "running"),
				func(s *terraform.State) error {
					rs := s.RootModule().Resources["gigahost_server.test"]
					if rs == nil {
						return errors.New("fixture server missing from state")
					}

					inputs := map[string]string{
						"GIGAHOST_TEST_SERVER_ID":    serverID,
						"GIGAHOST_TEST_SERVER_IP":    rs.Primary.Attributes["ip"],
						"GIGAHOST_TEST_SERVER_IP_ID": rs.Primary.Attributes["primary_ip_id"],
					}
					for name, value := range inputs {
						if value == "" {
							return fmt.Errorf("fixture has no value for %s", name)
						}
					}

					t.Logf("fixture_server_id=%s", serverID)

					recordFields := func() {
						srv, err := client.Servers.Get(accCtx, serverID)
						if err != nil {
							t.Logf("fixture field observation failed: %v", err)

							return
						}

						t.Logf("fixture fields: id=%s hostname=%q name=%q label=%q", srv.ID, srv.Hostname, srv.Name, srv.Label)
					}
					recordFields()

					failed := false

					for _, tc := range []struct {
						name string
						run  func(*testing.T)
					}{
						{"TestAccServerName_basic", TestAccServerName_basic},
						{"TestAccServerRDNS_IPv4", TestAccServerRDNS_IPv4},
						{"TestAccServerNameAndReverse", TestAccServerNameAndReverse},
						{"TestAccServerWithDNSRecord", TestAccServerWithDNSRecord},
						{"TestAccFullServerLifecycle", TestAccFullServerLifecycle},
						{"TestAccImportOOB_ServerName", TestAccImportOOB_ServerName},
						{"TestAccImportOOB_ServerRDNS", TestAccImportOOB_ServerRDNS},
						{"TestAccImportOOB_ServerSnapshot", TestAccImportOOB_ServerSnapshot},
					} {
						if !t.Run(tc.name, func(t *testing.T) {
							for name, value := range inputs {
								t.Setenv(name, value)
							}

							tc.run(t)
						}) {
							failed = true
						}

						recordFields()
					}

					if failed {
						return errors.New("existing-server acceptance subtests failed")
					}

					return nil
				},
			),
		}},
	})
}
