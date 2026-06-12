package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

func TestAccountAPIKeyLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// List.
	srv.Expect("GET", "/account/apikeys").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":[
				{
					"key_id":"1",
					"key_label":"CI deployment",
					"key_prefix":"flux_live_abc123",
					"permissions":{"dns":{"mode":"rw","all":true},"servers":{"mode":"r","all":true}},
					"created_at":1712000000,
					"expires_at":1740873600,
					"last_used_at":1712256000,
					"last_used_ip":"192.0.2.1",
					"status":"active",
					"revoked_at":null,
					"contact_id":"5"
				}
			]
		}`)

	// Get.
	srv.Expect("GET", "/account/apikeys/1").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{
				"key_id":"1",
				"key_label":"CI deployment",
				"key_prefix":"flux_live_abc123",
				"permissions":{"dns":{"mode":"rw","all":true}},
				"created_at":1712000000,
				"status":"active",
				"contact_id":"5"
			}
		}`)

	// Create.
	srv.Expect("POST", "/account/apikeys").
		WithJSON(`{"label":"My key","permissions":{"servers":{"mode":"r","all":true}}}`).
		Respond(http.StatusCreated, `{
			"meta":{"status":201,"status_message":"201 Created","message":"API key created."},
			"data":{
				"secret":"flux_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
				"prefix":"flux_live_xxxxxxxxxxxx",
				"label":"My key",
				"permissions":{"servers":{"mode":"r","all":true}}
			}
		}`)

	// Update.
	srv.Expect("PUT", "/account/apikeys/1").
		WithJSON(`{"label":"Renamed key"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Rotate.
	srv.Expect("POST", "/account/apikeys/1/rotate").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK","message":"API key rotated."},
			"data":{
				"secret":"flux_live_yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy",
				"prefix":"flux_live_yyyyyyyyyyyy"
			}
		}`)

	// Delete.
	srv.Expect("DELETE", "/account/apikeys/1").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	// ListAPIKeys.
	keys, err := c.Account.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("Account.ListAPIKeys: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}

	if keys[0].Label != "CI deployment" {
		t.Errorf("Label = %q", keys[0].Label)
	}

	if keys[0].Prefix != "flux_live_abc123" {
		t.Errorf("Prefix = %q", keys[0].Prefix)
	}

	if keys[0].Permissions.DNS == nil {
		t.Fatal("DNS permission should not be nil")
	}

	if keys[0].Permissions.DNS.Mode != "rw" {
		t.Errorf("DNS.Mode = %q", keys[0].Permissions.DNS.Mode)
	}

	if keys[0].LastUsedIP != "192.0.2.1" {
		t.Errorf("LastUsedIP = %q", keys[0].LastUsedIP)
	}

	// GetAPIKey.
	key, err := c.Account.GetAPIKey(ctx, "1")
	if err != nil {
		t.Fatalf("Account.GetAPIKey: %v", err)
	}

	if key.ID != "1" {
		t.Errorf("Key.ID = %q", key.ID)
	}

	// CreateAPIKey.
	created, err := c.Account.CreateAPIKey(ctx, client.CreateAPIKeyRequest{
		Label: "My key",
		Permissions: client.APIKeyPermissions{
			Servers: &client.APIKeyPermission{Mode: "r", All: true},
		},
	})
	if err != nil {
		t.Fatalf("Account.CreateAPIKey: %v", err)
	}

	if created.Secret == "" {
		t.Error("Secret should not be empty on create")
	}

	if created.Label != "My key" {
		t.Errorf("Label = %q", created.Label)
	}

	// UpdateAPIKey.
	if err := c.Account.UpdateAPIKey(ctx, "1", client.UpdateAPIKeyRequest{
		Label: "Renamed key",
	}); err != nil {
		t.Fatalf("Account.UpdateAPIKey: %v", err)
	}

	// RotateAPIKey.
	rotated, err := c.Account.RotateAPIKey(ctx, "1")
	if err != nil {
		t.Fatalf("Account.RotateAPIKey: %v", err)
	}

	if rotated.Secret == "" {
		t.Error("Rotated secret should not be empty")
	}

	if rotated.Prefix == "" {
		t.Error("Rotated prefix should not be empty")
	}

	// DeleteAPIKey.
	if err := c.Account.DeleteAPIKey(ctx, "1"); err != nil {
		t.Fatalf("Account.DeleteAPIKey: %v", err)
	}
}

func TestAccountAPIKeyValidation(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	ctx := context.Background()

	if _, err := c.Account.GetAPIKey(ctx, ""); err == nil {
		t.Error("expected error for empty keyID in GetAPIKey")
	}

	if _, err := c.Account.CreateAPIKey(ctx, client.CreateAPIKeyRequest{}); err == nil {
		t.Error("expected error for empty label in CreateAPIKey")
	}

	if err := c.Account.UpdateAPIKey(ctx, "", client.UpdateAPIKeyRequest{Label: "x"}); err == nil {
		t.Error("expected error for empty keyID in UpdateAPIKey")
	}

	if err := c.Account.UpdateAPIKey(ctx, "1", client.UpdateAPIKeyRequest{}); err == nil {
		t.Error("expected error when both label and permissions are zero")
	}

	if err := c.Account.DeleteAPIKey(ctx, ""); err == nil {
		t.Error("expected error for empty keyID in DeleteAPIKey")
	}

	if _, err := c.Account.RotateAPIKey(ctx, ""); err == nil {
		t.Error("expected error for empty keyID in RotateAPIKey")
	}
}
