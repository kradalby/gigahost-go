package tfprovider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestAccServerOSChangeMatrix exercises the os/iso/rescue transitions
// that have real API effects, against ONE server chain:
//
//	| # | transition          | expected action      | verified how                    |
//	|---|---------------------|----------------------|---------------------------------|
//	| 1 | (none) -> os A      | create               | apply                           |
//	| 2 | os A   -> os A      | no-op                | PlanOnly empty plan             |
//	| 3 | os A   -> os B      | update (reinstall)   | apply: same ID+IP, live OS == B |
//	| 4 | os B   -> rescue    | replace              | apply: new ID, old cancelled    |
//
// Pure classification of every transition (incl. iso-sourced state, which is
// unreachable live — the account has no uploaded ISOs) is covered offline by
// TestServerModifyPlanMatrix.
func TestAccServerOSChangeMatrix(t *testing.T) {
	client := testAccGigahostClient(t)
	typeSlug, sizeSlug := accCheapestTarget(t, client)
	osA := accPickOS(t, client)
	osB := accPickOSExcept(t, client, osA)
	pub, _ := accEphemeralKey(t)
	sshName := accRandName("oschange-ssh")

	var serverID, deployIP, rescueServerID string

	osConfig := func(osSlug string) string {
		return testAccServerConfig(sshName, pub, typeSlug, sizeSlug, osSlug)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviderFactories,
		CheckDestroy:             testAccCheckServerCancelled(client, &rescueServerID),
		Steps: []resource.TestStep{
			// 1. create with os A.
			{
				Config: osConfig(osA),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureAttr("gigahost_server.test", "id", &serverID),
					captureAttr("gigahost_server.test", "ip", &deployIP),
					resource.TestCheckResourceAttr("gigahost_server.test", "os", osA),
				),
			},
			// 2. same os: empty plan (PlanOnly fails on any diff).
			{
				Config:   osConfig(osA),
				PlanOnly: true,
			},
			// 3. os A -> os B: in-place reinstall. Same ID + IP, live OS flips.
			{
				Config: osConfig(osB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gigahost_server.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gigahost_server.test", "os", osB),
					testAccCheckServerUnchanged("gigahost_server.test", &serverID, &deployIP),
					testAccCheckServerOS(client, &serverID, osB),
				),
			},
			// 4. os B -> rescue: replace, applied for real. New server ID,
			// old server cancelled by the replace.
			{
				Config: testAccServerRescueConfig(sshName, pub, typeSlug, sizeSlug),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gigahost_server.test", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureAttr("gigahost_server.test", "id", &rescueServerID),
					testAccCheckServerReplaced(&serverID, &rescueServerID),
					testAccCheckServerCancelledByID(client, &serverID),
				),
			},
		},
	})
}

func testAccServerRescueConfig(sshName, pubKey, typeSlug, sizeSlug string) string {
	return fmt.Sprintf(`
%s

resource "gigahost_account_ssh_key" "test" {
  name       = %q
  public_key = %q
}

resource "gigahost_server" "test" {
  type     = %q
  size     = %q
  rescue   = true
  ssh_keys = [gigahost_account_ssh_key.test.id]
}
`, testAccProviderConfig(), sshName, pubKey, typeSlug, sizeSlug)
}

// accPickOSExcept returns an OS slug different from exclude.
func accPickOSExcept(t *testing.T, c *gigahost.Client, exclude string) string {
	t.Helper()

	all, err := c.Reinstall.ListAllOperatingSystems(accCtx)
	if err != nil {
		t.Fatalf("accPickOSExcept: %v", err)
	}

	for _, o := range all {
		if o.Slug != exclude {
			return o.Slug
		}
	}

	t.Fatal("accPickOSExcept: no alternate OS found")

	return ""
}

// testAccCheckServerOS verifies the live server runs the OS the slug
// resolves to (reinstall actually took effect).
func testAccCheckServerOS(client *gigahost.Client, serverID *string, wantSlug string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		want, err := client.Reinstall.ResolveOS(accCtx, wantSlug)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", wantSlug, err)
		}

		srv, err := client.Servers.Get(accCtx, *serverID)
		if err != nil {
			return fmt.Errorf("get server: %w", err)
		}

		if srv.OSID != want.OS.ID {
			return fmt.Errorf("server os_id = %q, want %q (%s; reinstall did not take)",
				srv.OSID, want.OS.ID, wantSlug)
		}

		return nil
	}
}

// testAccCheckServerUnchanged verifies the resource still has the originally
// captured server ID and IP (an in-place reinstall must not replace).
func testAccCheckServerUnchanged(addr string, wantID, wantIP *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not in state", addr)
		}

		if got := rs.Primary.ID; got != *wantID {
			return fmt.Errorf("server ID changed: %q -> %q (reinstall replaced the server)", *wantID, got)
		}

		if got := rs.Primary.Attributes["ip"]; got != *wantIP {
			return fmt.Errorf("server IP changed: %q -> %q (reinstall lost the IP)", *wantIP, got)
		}

		return nil
	}
}

// testAccCheckServerReplaced verifies the two captured IDs differ.
func testAccCheckServerReplaced(oldID, newID *string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if *oldID == "" || *newID == "" {
			return fmt.Errorf("missing IDs (old %q, new %q)", *oldID, *newID)
		}

		if *oldID == *newID {
			return fmt.Errorf("server %s was not replaced", *oldID)
		}

		return nil
	}
}

// testAccCheckServerCancelledByID verifies a specific server was cancelled
// (a second cancel must fail).
func testAccCheckServerCancelledByID(client *gigahost.Client, serverID *string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if err := client.Servers.Cancel(accCtx, *serverID); err == nil {
			return fmt.Errorf("old server %s was still cancellable; replace did not cancel it", *serverID)
		}

		return nil
	}
}
