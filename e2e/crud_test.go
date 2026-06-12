//go:build e2e

package e2e

import (
	"context"
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// TestSSHKeyLifecycle is the user's "basics" example #1: create an SSH key with
// the Go client, verify it, then tear it down and confirm it is gone.
func TestSSHKeyLifecycle(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	pub, _ := ephemeralKey(t)
	name := uniqueName("sshkey")

	if err := c.Account.AddSSHKey(ctx, name, pub); err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}

	// Safety-net teardown in case an assertion below aborts the test.
	t.Cleanup(func() {
		if id := findSSHKeyID(c, name); id != "" {
			_ = c.Account.DeleteSSHKey(context.Background(), id)
		}
	})

	id := findSSHKeyID(c, name)
	if id == "" {
		t.Fatalf("ssh key %q not found after add", name)
	}

	if err := c.Account.DeleteSSHKey(ctx, id); err != nil {
		t.Fatalf("DeleteSSHKey: %v", err)
	}

	if findSSHKeyID(c, name) != "" {
		t.Fatalf("ssh key %q still present after delete", name)
	}
}

// TestAPIKeyLifecycle creates a scoped API key, then gets, rotates, updates and
// deletes it, validating live state at each step. The create response omits the
// key ID, so we resolve it by prefix; the ID is stable across rotation while the
// prefix changes, so teardown keys on the ID.
func TestAPIKeyLifecycle(t *testing.T) {
	c := newClient(t)
	ctx := testContext(t)

	label := uniqueName("apikey")

	created, err := c.Account.CreateAPIKey(ctx, gigahost.CreateAPIKeyRequest{
		Label: label,
		Permissions: gigahost.APIKeyPermissions{
			DNS: &gigahost.APIKeyPermission{Mode: "r", All: true},
		},
	})
	if err != nil {
		skipIfForbidden(t, err)
		t.Fatalf("CreateAPIKey: %v", err)
	}

	if created.Secret == "" {
		t.Error("created API key has empty secret")
	}

	id := findAPIKeyIDByPrefix(c, created.Prefix)
	if id == "" {
		t.Fatalf("API key prefix %q not found after create", created.Prefix)
	}

	t.Cleanup(func() {
		if apiKeyExists(c, id) {
			_ = c.Account.DeleteAPIKey(context.Background(), id)
		}
	})

	got, err := c.Account.GetAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("GetAPIKey: %v", err)
	}

	if got.Label != label {
		t.Errorf("label = %q, want %q", got.Label, label)
	}

	if got.Permissions.DNS == nil || got.Permissions.DNS.Mode != "r" {
		t.Errorf("DNS permission = %+v, want mode r", got.Permissions.DNS)
	}

	rot, err := c.Account.RotateAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}

	if rot.Secret == "" || rot.Secret == created.Secret {
		t.Errorf("rotate did not produce a new secret")
	}

	newLabel := label + "-upd"
	if err := c.Account.UpdateAPIKey(ctx, id, gigahost.UpdateAPIKeyRequest{Label: newLabel}); err != nil {
		t.Fatalf("UpdateAPIKey: %v", err)
	}

	got2, err := c.Account.GetAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("GetAPIKey after update: %v", err)
	}

	if got2.Label != newLabel {
		t.Errorf("label after update = %q, want %q", got2.Label, newLabel)
	}

	if err := c.Account.DeleteAPIKey(ctx, id); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}

	if apiKeyExists(c, id) {
		t.Errorf("API key %s still present after delete", id)
	}
}
