package client_test

import (
	"context"
	"testing"

	client "github.com/kradalby/gigahost-go/client"
)

// Every service guards against an empty id, so a caller mistake becomes a
// clear error rather than a request to /servers//power/on — a path that
// addresses no server and whose response is anybody's guess. The guards were
// written but never exercised, so nothing would notice one being dropped.
//
// This matters more once the package is public: a third-party caller reaches
// these directly, without the provider's own validation in front.
func TestEmptyIDsAreRefused(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)
	ctx := context.Background()

	// Each entry performs no HTTP at all when the guard holds, which is what
	// the fake server asserts: any request here fails the test.
	calls := map[string]func() error{
		"Servers.Reboot":       func() error { return c.Servers.Reboot(ctx, "") },
		"Servers.PowerOn":      func() error { return c.Servers.PowerOn(ctx, "") },
		"Servers.PowerOff":     func() error { return c.Servers.PowerOff(ctx, "") },
		"Servers.Cancel":       func() error { return c.Servers.Cancel(ctx, "") },
		"Snapshots.Create":     func() error { return c.Snapshots.Create(ctx, "", "snap") },
		"Snapshots.Delete":     func() error { return c.Snapshots.Delete(ctx, "", 1) },
		"DNS.DeleteZone":       func() error { return c.DNS.DeleteZone(ctx, "") },
		"Account.DeleteSSHKey": func() error { return c.Account.DeleteSSHKey(ctx, "") },
		"Reinstall.Reinstall": func() error {
			_, err := c.Reinstall.Reinstall(ctx, "", client.ReinstallRequest{OSID: "88"})

			return err
		},
		"Servers.GetPowerState": func() error {
			_, err := c.Servers.GetPowerState(ctx, "")

			return err
		},
		"Servers.Get": func() error {
			_, err := c.Servers.Get(ctx, "")

			return err
		},
		"Snapshots.List": func() error {
			_, err := c.Snapshots.List(ctx, "")

			return err
		},
		"IPMI.Create": func() error {
			_, err := c.IPMI.Create(ctx, "", "203.0.113.4")

			return err
		},
		"DNS.ListRecords": func() error {
			_, err := c.DNS.ListRecords(ctx, "")

			return err
		},
		"Reinstall.ListOSes": func() error {
			_, err := c.Reinstall.ListOperatingSystems(ctx, "")

			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := call(); err == nil {
				t.Errorf("%s with an empty id: want an error, got nil", name)
			}
		})
	}
}
