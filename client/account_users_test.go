package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

func TestAccountUserLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// Get user.
	srv.Expect("GET", "/account/user/5").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{
				"contact_id":"5",
				"contact_name":"Jane Doe",
				"contact_username":"jane@example.com",
				"contact_access_level":"user",
				"contact_active":"1",
				"contact_2fa":"0",
				"servers":[
					{"id":"1","srv_id":"3523","srv_name":"web01","ip_address":"192.0.2.10"}
				],
				"servers_unassigned":[
					{"srv_id":"3524","srv_name":"db01","ip_address":"192.0.2.11"}
				]
			}
		}`)

	// Invite user.
	srv.Expect("POST", "/account").
		WithJSON(`{"name":"Bob Smith","username":"bob@example.com","accesslevel":"user"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Update user.
	srv.Expect("PUT", "/account/user/5").
		WithJSON(`{"contact_name":"Jane Smith","contact_username":"jane@example.com","contact_access_level":"admin"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Grant server access.
	srv.Expect("PUT", "/account/user/5/server").
		WithJSON(`{"srv_id":"3524"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Revoke server access.
	srv.Expect("DELETE", "/account/user/5/server/1").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	// Delete user.
	srv.Expect("DELETE", "/account/user/5").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	// Verify GetUser.
	u, err := c.Account.GetUser(ctx, "5")
	if err != nil {
		t.Fatalf("Account.GetUser: %v", err)
	}

	if u.ID != "5" {
		t.Errorf("UserDetails.ID = %q", u.ID)
	}

	if u.Name != "Jane Doe" {
		t.Errorf("UserDetails.Name = %q", u.Name)
	}

	if u.AccessLevel != "user" {
		t.Errorf("UserDetails.AccessLevel = %q", u.AccessLevel)
	}

	if !u.Active {
		t.Error("UserDetails.Active should be true")
	}

	if u.TwoFA {
		t.Error("UserDetails.TwoFA should be false")
	}

	if len(u.Servers) != 1 {
		t.Fatalf("want 1 assigned server, got %d", len(u.Servers))
	}

	if u.Servers[0].RelationID != "1" {
		t.Errorf("UserServer.RelationID = %q", u.Servers[0].RelationID)
	}

	if u.Servers[0].ServerID != "3523" {
		t.Errorf("UserServer.ServerID = %q", u.Servers[0].ServerID)
	}

	if len(u.ServersUnassigned) != 1 {
		t.Fatalf("want 1 unassigned server, got %d", len(u.ServersUnassigned))
	}

	// InviteUser.
	if err := c.Account.InviteUser(ctx, client.InviteUserRequest{
		Name:        "Bob Smith",
		Username:    "bob@example.com",
		AccessLevel: "user",
	}); err != nil {
		t.Fatalf("Account.InviteUser: %v", err)
	}

	// UpdateUser.
	if err := c.Account.UpdateUser(ctx, "5", client.UpdateUserRequest{
		Name:        "Jane Smith",
		Username:    "jane@example.com",
		AccessLevel: "admin",
	}); err != nil {
		t.Fatalf("Account.UpdateUser: %v", err)
	}

	// GrantServerAccess.
	if err := c.Account.GrantServerAccess(ctx, "5", "3524"); err != nil {
		t.Fatalf("Account.GrantServerAccess: %v", err)
	}

	// RevokeServerAccess.
	if err := c.Account.RevokeServerAccess(ctx, "5", "1"); err != nil {
		t.Fatalf("Account.RevokeServerAccess: %v", err)
	}

	// DeleteUser.
	if err := c.Account.DeleteUser(ctx, "5"); err != nil {
		t.Fatalf("Account.DeleteUser: %v", err)
	}
}

func TestAccountUserValidation(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	ctx := context.Background()

	if _, err := c.Account.GetUser(ctx, ""); err == nil {
		t.Error("expected error for empty userID in GetUser")
	}

	if err := c.Account.InviteUser(ctx, client.InviteUserRequest{}); err == nil {
		t.Error("expected error for empty InviteUserRequest")
	}

	if err := c.Account.DeleteUser(ctx, ""); err == nil {
		t.Error("expected error for empty userID in DeleteUser")
	}

	if err := c.Account.GrantServerAccess(ctx, "", "3523"); err == nil {
		t.Error("expected error for empty userID in GrantServerAccess")
	}

	if err := c.Account.GrantServerAccess(ctx, "5", ""); err == nil {
		t.Error("expected error for empty serverID in GrantServerAccess")
	}

	if err := c.Account.RevokeServerAccess(ctx, "", "1"); err == nil {
		t.Error("expected error for empty userID in RevokeServerAccess")
	}

	if err := c.Account.RevokeServerAccess(ctx, "5", ""); err == nil {
		t.Error("expected error for empty relationID in RevokeServerAccess")
	}
}
