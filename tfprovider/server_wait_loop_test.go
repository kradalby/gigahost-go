package tfprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// fakeAPI serves the two endpoints waitForServer/findServerConfirmed touch:
// GET /deploy/status and GET /servers/{id}. statusFn is called with the 1-based
// status-poll count; serverFn with the server id. Each returns a JSON body and
// an HTTP status code (non-200 becomes an APIError on the client side).
type fakeAPI struct {
	statusCalls atomic.Int64
	statusFn    func(n int64) (body string, code int)
	serverFn    func(id string) (body string, code int)
}

func (f *fakeAPI) server(t *testing.T) *gigahost.Client {
	t.Helper()

	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/deploy/status":
			n := f.statusCalls.Add(1)
			body, code := f.statusFn(n)
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		case strings.HasPrefix(r.URL.Path, "/servers/"):
			id := strings.TrimPrefix(r.URL.Path, "/servers/")
			body, code := f.serverFn(id)
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	c, err := gigahost.NewClient(
		gigahost.WithToken("t"),
		gigahost.WithBaseURL("http://fake.gigahost.test"),
		gigahost.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return c
}

func statusEnvelope(order, srvID, status string, allReady bool) string {
	return fmt.Sprintf(`{"data":{"all_ready":%t,"servers":[{"order_id":%q,"srv_id":%q,"status":%q,"ip":"192.0.2.1"}]}}`,
		allReady, order, srvID, status)
}

func emptyStatusEnvelope() string {
	return `{"data":{"all_ready":false,"servers":[]}}`
}

func serverEnvelope(id string, install, running bool) string {
	return fmt.Sprintf(`{"data":[{"srv_id":%q,"srv_status":%t,"srv_status_install":%t,"srv_primary_ip":"192.0.2.1","ips":[]}]}`,
		id, running, install)
}

func testResource(c *gigahost.Client) *serverResource {
	return &serverResource{client: c}
}

func TestWaitForServer_HappyPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) {
				if n == 1 {
					return statusEnvelope("7", "s7", "installing", false), 200
				}

				return statusEnvelope("7", "s7", "ready", true), 200
			},
			serverFn: func(id string) (string, int) { return serverEnvelope(id, false, true), 200 },
		}
		r := testResource(api.server(t))

		start := time.Now()

		res, err := r.waitForServer(context.Background(), "7", deployReadyTimeout)
		if err != nil {
			t.Fatalf("waitForServer err: %v", err)
		}

		if res.serverID != "s7" {
			t.Fatalf("result = %+v", res)
		}

		// First poll is immediate and reports "installing"; the second reports
		// ready. So the wait costs exactly one production poll interval — the
		// assertion that stops deployPollInterval being quietly shrunk to keep
		// a wall-clock test fast.
		assertElapsed(t, start, deployPollInterval)
	})
}

func TestWaitForServer_ListFallbackCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Status reports the server id once, then drops the order; the durable
		// /servers record reports completion.
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) {
				if n == 1 {
					return statusEnvelope("7", "s7", "installing", false), 200
				}

				return emptyStatusEnvelope(), 200
			},
			serverFn: func(id string) (string, int) { return serverEnvelope(id, false, true), 200 },
		}
		r := testResource(api.server(t))

		res, err := r.waitForServer(context.Background(), "7", deployReadyTimeout)
		if err != nil {
			t.Fatalf("waitForServer err: %v", err)
		}

		if res.serverID != "s7" {
			t.Fatalf("server id not preserved: %+v", res)
		}
	})
}

func TestWaitForServer_DisappearedFailsButKeepsID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Server id is seen once, then the order drops and the server record 404s
		// forever: the wait must fail yet return the id so destroy can cancel it.
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) {
				if n == 1 {
					return statusEnvelope("7", "s7", "installing", false), 200
				}

				return emptyStatusEnvelope(), 200
			},
			serverFn: func(id string) (string, int) { return `{"meta":{"message":"gone"}}`, 404 },
		}
		r := testResource(api.server(t))

		res, err := r.waitForServer(context.Background(), "7", deployReadyTimeout)
		if err == nil {
			t.Fatal("expected error when server disappears")
		}

		if res == nil || res.serverID != "s7" {
			t.Fatalf("server id not preserved on failure: %+v", res)
		}
	})
}

func TestWaitForServer_ToleratesTransientStatusErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) {
				if n <= maxStatusPollErrors {
					return `{}`, 500
				}

				return statusEnvelope("7", "s7", "ready", true), 200
			},
			serverFn: func(id string) (string, int) { return serverEnvelope(id, false, true), 200 },
		}
		r := testResource(api.server(t))

		res, err := r.waitForServer(context.Background(), "7", deployReadyTimeout)
		if err != nil {
			t.Fatalf("transient status errors should be tolerated: %v", err)
		}

		if res.serverID != "s7" {
			t.Fatalf("result = %+v", res)
		}
	})
}

func TestWaitForServer_SustainedStatusErrorsFail(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) { return `{}`, 500 },
			serverFn: func(id string) (string, int) { return serverEnvelope(id, false, true), 200 },
		}
		r := testResource(api.server(t))

		if _, err := r.waitForServer(context.Background(), "7", deployReadyTimeout); err == nil {
			t.Fatal("sustained status errors should fail the wait")
		}
	})
}

func TestWaitForServer_ExplicitFailureStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) { return statusEnvelope("7", "s7", "failed", false), 200 },
			serverFn: func(id string) (string, int) { return serverEnvelope(id, true, false), 200 },
		}
		r := testResource(api.server(t))

		res, err := r.waitForServer(context.Background(), "7", deployReadyTimeout)
		if err == nil {
			t.Fatal("a failure status should fail fast")
		}

		if res == nil || res.serverID != "s7" {
			t.Fatalf("id not preserved: %+v", res)
		}
	})
}

func TestFindServerConfirmed_TransientMissThenFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		api := &fakeAPI{
			statusFn: func(n int64) (string, int) { return emptyStatusEnvelope(), 200 },
			serverFn: func(id string) (string, int) {
				if calls.Add(1) <= 2 {
					return `{"meta":{"message":"gap"}}`, 404
				}

				return serverEnvelope(id, false, true), 200
			},
		}
		r := testResource(api.server(t))

		srv, err := r.findServerConfirmed(context.Background(), "s7")
		if err != nil {
			t.Fatalf("transient miss should be retried: %v", err)
		}

		if srv == nil || srv.ID != "s7" {
			t.Fatalf("server = %+v", srv)
		}
	})
}

func TestFindServerConfirmed_ConfirmedAbsent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &fakeAPI{
			statusFn: func(n int64) (string, int) { return emptyStatusEnvelope(), 200 },
			serverFn: func(id string) (string, int) { return `{"meta":{"message":"gone"}}`, 404 },
		}
		r := testResource(api.server(t))

		_, err := r.findServerConfirmed(context.Background(), "s7")
		if !gigahost.IsNotFound(err) {
			t.Fatalf("confirmed absence should report not-found, got %v", err)
		}
	})
}
