package client_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// newTestClient builds a client pointed at the given test server with a
// fixed bearer token. Service-level tests use this to get a
// ready-to-use Client in one line.
func newTestClient(t *testing.T, srv *testhelper.Server, opts ...client.Option) *client.Client {
	t.Helper()

	all := append([]client.Option{
		client.WithToken("test-token"),
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
	}, opts...)

	c, err := client.NewClient(all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return c
}

func TestNewClientRejectsMissingAuth(t *testing.T) {
	t.Parallel()

	_, err := client.NewClient()
	if err == nil {
		t.Fatal("expected error when no auth option is provided")
	}
}

func TestNewClientAppliesDefaults(t *testing.T) {
	t.Parallel()

	c, err := client.NewClient(client.WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if c.BaseURL() != client.DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL(), client.DefaultBaseURL)
	}
}

func TestWithBaseURL(t *testing.T) {
	t.Parallel()

	c, err := client.NewClient(
		client.WithToken("t"),
		client.WithBaseURL("https://api.example.com/v1"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if c.BaseURL() != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}

func TestAPIErrorImplementsIs(t *testing.T) {
	t.Parallel()

	apiErr := &client.APIError{StatusCode: http.StatusNotFound, Message: "nope"}

	if !errors.Is(apiErr, client.ErrNotFound) {
		t.Error("APIError should match ErrNotFound")
	}

	if errors.Is(apiErr, client.ErrConflict) {
		t.Error("APIError should not match ErrConflict")
	}

	if !strings.Contains(apiErr.Error(), "nope") {
		t.Errorf("APIError.Error() missing message: %s", apiErr.Error())
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	notFound := &client.APIError{StatusCode: http.StatusNotFound, Message: "gone"}
	conflict := &client.APIError{StatusCode: http.StatusConflict}
	serverErr := &client.APIError{StatusCode: http.StatusInternalServerError}

	if !client.IsNotFound(notFound) {
		t.Error("IsNotFound(404) should return true")
	}

	if client.IsNotFound(conflict) {
		t.Error("IsNotFound(409) should return false")
	}

	if client.IsNotFound(serverErr) {
		t.Error("IsNotFound(500) should return false")
	}

	if client.IsNotFound(nil) {
		t.Error("IsNotFound(nil) should return false")
	}
}

func TestAuthenticateFlow(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	expire := time.Now().Add(time.Hour).Unix()

	srv.Expect("POST", "/authenticate").
		WithJSON(`{"username":"a@b","password":"s3cret"}`).
		RespondJSON(http.StatusOK, map[string]any{
			"meta": map[string]any{"status": 200, "status_message": "200 OK"},
			"data": map[string]any{"token": "new-token", "token_expire": expire, "customer_id": "1234"},
		})

	c, err := client.NewClient(
		client.WithCredentials("a@b", "s3cret", 0),
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tok, err := c.Auth.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if tok.Token != "new-token" {
		t.Errorf("Token = %q", tok.Token)
	}

	if tok.CustomerID != "1234" {
		t.Errorf("CustomerID = %q", tok.CustomerID)
	}
}

func TestErrorResponseIsMappedToAPIError(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	srv.Expect("POST", "/authenticate").
		Respond(http.StatusUnauthorized, `{"meta":{"status":401,"status_message":"401 Unauthorized","message":"bad creds"}}`)

	c, err := client.NewClient(
		client.WithCredentials("a@b", "wrong", 0),
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}

	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}

	if apiErr.Message != "bad creds" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}
