package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kradalby/gigahost-go/cli"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	code := cli.Run(context.Background(), []string{"version"}, cli.Options{
		Version: "1.2.3",
		Commit:  "abc1234",
		Stdout:  &out,
		Stderr:  &out,
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, out.String())
	}

	if !strings.Contains(out.String(), "1.2.3") {
		t.Errorf("output missing version: %q", out.String())
	}

	if !strings.Contains(out.String(), "abc1234") {
		t.Errorf("output missing commit: %q", out.String())
	}
}

func TestHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	code := cli.Run(context.Background(), []string{"-h"}, cli.Options{
		Stdout: &out,
		Stderr: &out,
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out.String(), "gigahost") {
		t.Errorf("help output looks wrong: %q", out.String())
	}
}

// TestCommandTree walks the full command tree and verifies every
// subcommand produces help text without error. This catches
// programming mistakes where a new subcommand was registered but its
// flag set references something undefined, or where a parent flag
// set wasn't correctly wired through SetParent.
func TestCommandTree(t *testing.T) {
	t.Parallel()

	paths := [][]string{
		{"deploy", "-h"},
		{"deploy", "catalog", "-h"},
		{"deploy", "create", "-h"},
		{"deploy", "status", "-h"},
		{"deploy", "isos", "-h"},
		{"dns", "-h"},
		{"dns", "zones", "-h"},
		{"dns", "records", "-h"},
		{"dns", "redirects", "-h"},
		{"dns", "dnssec", "-h"},
		{"dns", "domain", "-h"},
		{"dns", "domain", "register", "-h"},
		{"dns", "registrant", "-h"},
		{"dns", "registrant", "set-email", "-h"},
		{"dns", "registrant", "auto-renew", "-h"},
		{"dns", "nameservers", "-h"},
		{"dns", "ptr", "-h"},
		{"dns", "dyndns", "-h"},
		{"servers", "-h"},
		{"servers", "power", "-h"},
		{"servers", "reverse", "-h"},
		{"servers", "ip-order", "-h"},
		{"servers", "graphs", "-h"},
		{"servers", "snapshots", "-h"},
		{"servers", "reinstall", "-h"},
		{"servers", "ipmi", "-h"},
		{"servers", "isos", "-h"},
		{"servers", "upgrades", "-h"},
		{"bgp", "-h"},
		{"bgp", "asn", "-h"},
		{"bgp", "session", "-h"},
		{"account", "-h"},
		{"account", "balance", "-h"},
		{"account", "users", "-h"},
		{"account", "users", "list", "-h"},
		{"account", "users", "get", "-h"},
		{"account", "users", "invite", "-h"},
		{"account", "users", "update", "-h"},
		{"account", "users", "delete", "-h"},
		{"account", "users", "grant-server", "-h"},
		{"account", "users", "revoke-server", "-h"},
		{"account", "ssh-keys", "-h"},
		{"account", "ssh-keys", "list", "-h"},
		{"account", "ssh-keys", "add", "-h"},
		{"account", "ssh-keys", "delete", "-h"},
		{"account", "api-keys", "-h"},
		{"account", "api-keys", "list", "-h"},
		{"account", "api-keys", "get", "-h"},
		{"account", "api-keys", "create", "-h"},
		{"account", "api-keys", "update", "-h"},
		{"account", "api-keys", "delete", "-h"},
		{"account", "api-keys", "rotate", "-h"},
		{"auth", "-h"},
	}

	for _, args := range paths {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			code := cli.Run(context.Background(), args, cli.Options{
				Stdout: &out,
				Stderr: &out,
			})
			if code != 0 {
				t.Fatalf("exit = %d\nout:\n%s", code, out.String())
			}
		})
	}
}
