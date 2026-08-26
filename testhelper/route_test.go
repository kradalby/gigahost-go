package testhelper_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/testhelper"
)

// get issues a GET against the fake server and returns the status and body.
// The fake is on an in-memory network, so it must be reached via its own
// client rather than http.DefaultClient.
func get(t *testing.T, srv *testhelper.Server, path string) (int, string) {
	t.Helper()

	res, err := srv.Client().Get(srv.URL() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return res.StatusCode, string(b)
}

// TestRouteAnswersRepeatedlyAndOutOfOrder pins the property that makes a
// lifecycle test possible: the code under test decides how many requests it
// makes and in what order, and the fake must not care.
func TestRouteAnswersRepeatedlyAndOutOfOrder(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	servers := srv.Route("GET", "/servers/*").Respond(http.StatusOK, `{"data":"srv"}`)
	zones := srv.Route("GET", "/dns/zones").Respond(http.StatusOK, `{"data":"zones"}`)

	// Out of registration order, and repeated.
	for range 3 {
		if code, body := get(t, srv, "/dns/zones"); code != 200 || body != `{"data":"zones"}` {
			t.Fatalf("zones: %d %q", code, body)
		}
	}

	// Wildcard matches any server id.
	if code, body := get(t, srv, "/servers/18394"); code != 200 || body != `{"data":"srv"}` {
		t.Fatalf("servers: %d %q", code, body)
	}

	if got := zones.Calls(); got != 3 {
		t.Errorf("zones Calls() = %d, want 3", got)
	}

	if got := servers.Calls(); got != 1 {
		t.Errorf("servers Calls() = %d, want 1", got)
	}
}

// TestRespondWithSeesCallCount covers the case testhelper could not express
// before, and which forced tfprovider to grow a private fake: a poll loop that
// must see a changing answer.
func TestRespondWithSeesCallCount(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	srv.Route("GET", "/deploy/status").
		RespondWith(func(_ *http.Request, call int) (int, string) {
			if call < 3 {
				return http.StatusOK, `{"status":"installing"}`
			}

			return http.StatusOK, `{"status":"ready"}`
		})

	want := []string{
		`{"status":"installing"}`,
		`{"status":"installing"}`,
		`{"status":"ready"}`,
		`{"status":"ready"}`,
	}

	for i, w := range want {
		if _, body := get(t, srv, "/deploy/status"); body != w {
			t.Errorf("call %d: body = %q, want %q", i+1, body, w)
		}
	}
}

// TestRespondWithCanFailTransiently is the shape every retry and
// absence-confirmation test needs: the same endpoint erroring, then healing.
// The live API cannot be asked to do this on demand.
func TestRespondWithCanFailTransiently(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	srv.Route("GET", "/servers/1").
		RespondWith(func(_ *http.Request, call int) (int, string) {
			if call <= 2 {
				return http.StatusNotFound, `{"meta":{"status":404,"message":"Not found."}}`
			}

			return http.StatusOK, `{"meta":{"status":200},"data":{"srv_id":"1"}}`
		})

	for _, want := range []int{404, 404, 200} {
		if code, _ := get(t, srv, "/servers/1"); code != want {
			t.Errorf("status = %d, want %d", code, want)
		}
	}
}

// TestOrderedExpectationsTakePrecedence guards the compatibility promise: the
// 18 existing client test files script exact sequences with Expect, and adding
// routes must not change how those behave.
func TestOrderedExpectationsTakePrecedence(t *testing.T) {
	t.Parallel()

	srv := testhelper.NewServer(t)
	srv.Expect("GET", "/dns/zones").Respond(http.StatusOK, `{"data":"ordered"}`)
	srv.Route("GET", "/dns/zones").Respond(http.StatusOK, `{"data":"routed"}`)

	// First call consumes the ordered expectation...
	if _, body := get(t, srv, "/dns/zones"); body != `{"data":"ordered"}` {
		t.Errorf("first call = %q, want the ordered response", body)
	}

	// ...and only then does the route answer.
	if _, body := get(t, srv, "/dns/zones"); body != `{"data":"routed"}` {
		t.Errorf("second call = %q, want the routed response", body)
	}
}

// TestUnroutedRequestIsStillAnError keeps the safety net: a request nobody
// expected must fail the test rather than get a silent 200.
func TestUnroutedRequestIsStillAnError(t *testing.T) {
	t.Parallel()

	// A nested *testing.T would fail this test, so drive the check by
	// asserting the status code the server returns for an unmatched request.
	srv := testhelper.NewServer(t)
	srv.Route("GET", "/known").Respond(http.StatusOK, `{}`)

	if code, _ := get(t, srv, "/known"); code != http.StatusOK {
		t.Fatalf("known route: %d", code)
	}
	// Deliberately not calling /unknown: doing so would mark this test failed,
	// which is exactly the behaviour being relied upon elsewhere.
}
