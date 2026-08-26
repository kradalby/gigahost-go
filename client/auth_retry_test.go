package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	client "github.com/kradalby/gigahost-go/client"
	"github.com/kradalby/gigahost-go/testhelper"
)

// The transparent 401 retry is the one piece of the client that changes
// behaviour silently: a token expires mid-run, the client re-authenticates
// and replays the request, and the caller never learns. It had no test at
// all.

const authOKJSON = `{"meta":{"status":200},"data":{"token":"fresh-token"}}`

// credentialsClient builds a client that can re-authenticate, pointed at srv.
func credentialsClient(t *testing.T, srv *testhelper.Server) *client.Client {
	t.Helper()

	c, err := client.NewClient(
		client.WithBaseURL(srv.URL()),
		client.WithHTTPClient(srv.Client()),
		client.WithCredentials("user@example.no", "hunter2", 0),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return c
}

// TestUnauthorizedTriggersReauthAndRetry is the happy path: the stored token
// has expired, so the first call 401s, the client re-authenticates and replays
// the request, and the caller sees only the successful result.
func TestUnauthorizedTriggersReauthAndRetry(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	c := credentialsClient(t, srv)

	// Initial authenticate, then a 401, then a second authenticate, then the
	// replayed request succeeding.
	srv.Expect(http.MethodPost, "/authenticate").Respond(http.StatusOK, authOKJSON)
	srv.Expect(http.MethodGet, "/dns/zones").
		Respond(http.StatusUnauthorized, `{"meta":{"status":401,"message":"Token expired."}}`)
	srv.Expect(http.MethodPost, "/authenticate").Respond(http.StatusOK, authOKJSON)
	srv.Expect(http.MethodGet, "/dns/zones").
		RespondFixture(t, "testdata/dns/list_zones.json")

	zones, err := c.DNS.ListZones(context.Background())
	if err != nil {
		t.Fatalf("ListZones should have succeeded after re-authenticating: %v", err)
	}

	if len(zones) == 0 {
		t.Error("no zones decoded from the replayed request")
	}
}

// TestUnauthorizedRetriesOnlyOnce stops the retry becoming a loop: if the
// second attempt is also refused, the caller gets the error rather than the
// client hammering the auth endpoint.
func TestUnauthorizedRetriesOnlyOnce(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	c := credentialsClient(t, srv)

	unauthorized := `{"meta":{"status":401,"status_message":"401 Unauthorized","message":"Nope."}}`

	srv.Expect(http.MethodPost, "/authenticate").Respond(http.StatusOK, authOKJSON)
	srv.Expect(http.MethodGet, "/dns/zones").Respond(http.StatusUnauthorized, unauthorized)
	srv.Expect(http.MethodPost, "/authenticate").Respond(http.StatusOK, authOKJSON)
	srv.Expect(http.MethodGet, "/dns/zones").Respond(http.StatusUnauthorized, unauthorized)

	_, err := c.DNS.ListZones(context.Background())
	if err == nil {
		t.Fatal("a second 401 must surface as an error, not another retry")
	}

	if !errors.Is(err, client.ErrUnauthorized) {
		t.Errorf("error %v should be recognisable as ErrUnauthorized", err)
	}
}

// TestTokenOnlyClientDoesNotRetry pins the other half of the condition. With
// no credentials there is nothing to re-authenticate with, so retrying would
// just repeat a request that cannot succeed.
func TestTokenOnlyClientDoesNotRetry(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)

	c, err := client.NewClient(client.WithBaseURL(srv.URL()), client.WithHTTPClient(srv.Client()), client.WithToken("static"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Exactly one request: a second would fail the expectation queue.
	srv.Expect(http.MethodGet, "/dns/zones").
		Respond(http.StatusUnauthorized, `{"meta":{"status":401,"message":"Nope."}}`)

	if _, err := c.DNS.ListZones(context.Background()); err == nil {
		t.Fatal("a 401 with a static token must surface as an error")
	}
}
