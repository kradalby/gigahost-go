package testhelper

import (
	"net/http"
	"strings"
)

// Route registers an order-independent handler for a method and path.
//
// Routes are the counterpart to Expect: where Expect scripts an exact
// sequence, a Route answers whenever it is asked, however many times, in
// whatever order. That is what a lifecycle test needs, because the number
// and order of requests is decided by the code under test — a poll loop
// that retries, a fallback that only fires every third miss — and pinning
// it in the test would assert the implementation rather than the
// behaviour.
//
// Ordered expectations always win: a Route is consulted only once the
// Expect queue is exhausted, so registering routes cannot change the
// behaviour of a test that does not use them.
//
// The path matches exactly, unless it ends in "*", which matches any
// suffix: Route("GET", "/servers/*") answers for every server id.
//
// Unlike an Expect, a Route that is never called is not an error.
func (s *Server) Route(method, path string) *Expectation {
	s.t.Helper()

	e := &Expectation{
		server:       s,
		method:       method,
		path:         path,
		routed:       true,
		responseCode: http.StatusOK,
		responseBody: []byte(`{"meta":{"status":200,"status_message":"200 OK"}}`),
	}

	s.mu.Lock()
	s.routes = append(s.routes, e)
	s.mu.Unlock()

	return e
}

// RespondWith answers with whatever fn returns, so a response can depend
// on how many times it has been asked or on what earlier requests did.
//
// The callback receives the request and the 1-based call count for this
// expectation, and returns an HTTP status and a body. It takes precedence
// over any body set by Respond or RespondFixture.
//
//	srv.Route("GET", "/deploy/status").
//		RespondWith(func(_ *http.Request, n int) (int, string) {
//			if n < 3 {
//				return 200, installing
//			}
//			return 200, ready
//		})
func (e *Expectation) RespondWith(fn func(r *http.Request, call int) (int, string)) *Expectation {
	e.respondFn = fn

	return e
}

// Calls reports how many requests this expectation has answered. Useful
// for asserting that a retry or a fallback actually happened.
func (e *Expectation) Calls() int {
	e.server.mu.Lock()
	defer e.server.mu.Unlock()

	return e.calls
}

// matchesRoute reports whether this route should answer r. Ordered
// expectations use match instead, which also verifies the body and query.
func (e *Expectation) matchesRoute(r *http.Request) bool {
	if e.method != r.Method {
		return false
	}

	if prefix, ok := strings.CutSuffix(e.path, "*"); ok {
		return strings.HasPrefix(r.URL.Path, prefix)
	}

	return e.path == r.URL.Path
}
