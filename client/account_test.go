package client_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kradalby/gigahost-go/client"
)

func TestAccountGet(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/account").
		RespondFixture(t, "testdata/account/get.json")

	acc, err := c.Account.Get(context.Background())
	if err != nil {
		t.Fatalf("Account.Get: %v", err)
	}

	if acc.CustomerID != "1111" {
		t.Errorf("CustomerID = %q", acc.CustomerID)
	}

	if acc.BillingEmail != "billing@b.no" {
		t.Errorf("BillingEmail = %q", acc.BillingEmail)
	}

	if !acc.Incident {
		t.Error("Incident notification should be true")
	}

	if acc.Newsletter {
		t.Error("Newsletter should be false")
	}

	if acc.IsPartner {
		t.Error("IsPartner should be false")
	}

	// SSH keys.
	if len(acc.SSHKeys) != 1 {
		t.Fatalf("want 1 SSH key, got %d", len(acc.SSHKeys))
	}

	if acc.SSHKeys[0].ID != "5" {
		t.Errorf("SSHKey.ID = %q", acc.SSHKeys[0].ID)
	}

	if acc.SSHKeys[0].Name != "laptop" {
		t.Errorf("SSHKey.Name = %q", acc.SSHKeys[0].Name)
	}

	wantAdded := time.Unix(1700000000, 0).UTC()
	if !acc.SSHKeys[0].AddedAt.Equal(wantAdded) {
		t.Errorf("SSHKey.AddedAt = %v, want %v", acc.SSHKeys[0].AddedAt, wantAdded)
	}

	// Passkeys.
	if len(acc.Passkeys) != 1 {
		t.Fatalf("want 1 passkey, got %d", len(acc.Passkeys))
	}

	if acc.Passkeys[0].Name != "YubiKey" {
		t.Errorf("Passkey.Name = %q", acc.Passkeys[0].Name)
	}

	// Contacts — new shape: AccessLevel, TwoFA.
	if len(acc.Contacts) != 1 {
		t.Fatalf("want 1 contact, got %d", len(acc.Contacts))
	}

	if acc.Contacts[0].AccessLevel != "admin" {
		t.Errorf("Contact.AccessLevel = %q", acc.Contacts[0].AccessLevel)
	}

	if !acc.Contacts[0].TwoFA {
		t.Error("Contact.TwoFA should be true (parsed from \"1\")")
	}

	// Orders.
	if len(acc.Orders) != 1 {
		t.Fatalf("want 1 order, got %d", len(acc.Orders))
	}

	if acc.Orders[0].Number != "GH-100010" {
		t.Errorf("Order.Number = %q", acc.Orders[0].Number)
	}

	if len(acc.Orders[0].Products) != 1 {
		t.Fatalf("want 1 order product, got %d", len(acc.Orders[0].Products))
	}

	if acc.Orders[0].Products[0].ServerID != "3523" {
		t.Errorf("OrderProduct.ServerID = %q", acc.Orders[0].Products[0].ServerID)
	}
}

func TestAccountUpdateAddress(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/account").
		WithJSON(`{"cust_zipcode":"5000","cust_city":"Bergen","cust_country":"NO"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.Account.UpdateAddress(context.Background(), client.UpdateAddressRequest{
		ZipCode: "5000",
		City:    "Bergen",
		Country: "NO",
	})
	if err != nil {
		t.Fatalf("Account.UpdateAddress: %v", err)
	}
}

func TestAccountUpdateNotifications(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/account/notifications").
		WithJSON(`{"cust_newsletter":true,"cust_incident":true,"cust_bandwidth_notification":false,"cust_email_on_login":false,"cust_notify_service_renewal":true}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.Account.UpdateNotifications(context.Background(), client.NotificationPrefs{
		Newsletter:            true,
		Incident:              true,
		BandwidthNotification: false,
		EmailOnLogin:          false,
		NotifyServiceRenewal:  true,
	})
	if err != nil {
		t.Fatalf("Account.UpdateNotifications: %v", err)
	}
}

func TestAccountGetActivity(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/account/activity").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":[
				{
					"log_id":"99",
					"contact_id":"1",
					"contact_name":"Admin",
					"contact_username":"admin@b.no",
					"log_timestamp":1712000000,
					"log_entry":"User changed their password."
				}
			]
		}`)

	entries, err := c.Account.GetActivity(context.Background())
	if err != nil {
		t.Fatalf("Account.GetActivity: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	if entries[0].ID != "99" {
		t.Errorf("Entry.ID = %q", entries[0].ID)
	}

	if entries[0].Entry != "User changed their password." {
		t.Errorf("Entry.Entry = %q", entries[0].Entry)
	}

	wantTS := time.Unix(1712000000, 0).UTC()
	if !entries[0].Timestamp.Equal(wantTS) {
		t.Errorf("Entry.Timestamp = %v, want %v", entries[0].Timestamp, wantTS)
	}
}

func TestAccountSSHKeyLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/account/sshkey").
		WithJSON(`{"name":"laptop","data":"ssh-ed25519 AAAA... user@host"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	srv.Expect("DELETE", "/account/sshkey/5").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	if err := c.Account.AddSSHKey(ctx, "laptop", "ssh-ed25519 AAAA... user@host"); err != nil {
		t.Fatalf("Account.AddSSHKey: %v", err)
	}

	if err := c.Account.DeleteSSHKey(ctx, "5"); err != nil {
		t.Fatalf("Account.DeleteSSHKey: %v", err)
	}
}

func TestAccountChangePassword(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/account/password").
		WithJSON(`{"current":"oldpass","new":"newpass"}`).
		Respond(http.StatusCreated, `{"meta":{"status":201,"status_message":"201 Created"}}`)

	if err := c.Account.ChangePassword(context.Background(), "oldpass", "newpass"); err != nil {
		t.Fatalf("Account.ChangePassword: %v", err)
	}
}

func TestInvoicesList(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/my/invoices").
		Respond(http.StatusOK, `{"success":true,"invoices":[{"inv_id":"1","order_id":"2","order_number":"3","inv_md5":"abc","inv_filename":"Invoice_1.pdf","inv_number":"4","inv_date":"1700000000","inv_duedate":"1702592000","inv_paid":"1","inv_total":"100.00","inv_vat":"25","inv_total_vat":"125"}]}`)

	invoices, err := c.Invoices.List(context.Background())
	if err != nil {
		t.Fatalf("Invoices.List: %v", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("want 1 invoice, got %d", len(invoices))
	}

	if !invoices[0].Paid {
		t.Error("Paid should be true")
	}

	wantDate := time.Unix(1700000000, 0).UTC()
	if !invoices[0].Date.Equal(wantDate) {
		t.Errorf("Date = %v, want %v", invoices[0].Date, wantDate)
	}
}
