package testhelper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Server wraps an httptest.Server and a FIFO queue of expectations.
//
// Expectations are matched in the order they are registered. This keeps
// tests deterministic and makes it easy to script the exact sequence the
// client is expected to perform during a lifecycle test.
type Server struct {
	t *testing.T

	mu           sync.Mutex
	expectations []*Expectation
	routes       []*Expectation
	server       *httptest.Server
	index        int
	unmatched    int
}

// NewServer constructs a Server registered for cleanup with t.
//
// The fake runs on httptest's in-memory network, not a real TCP listener, so
// it works inside a [testing/synctest] bubble. Requests must therefore be
// issued with the [Server.Client] returned here — an ordinary
// [http.DefaultClient] cannot reach it.
func NewServer(t *testing.T) *Server {
	t.Helper()

	s := &Server{t: t}
	s.server = httptest.NewTestServer(t, http.HandlerFunc(s.handle))

	// NewTestServer registers its own Close, and cleanups run LIFO, so
	// AssertDone now runs before the server closes rather than after. That is
	// safe here because every request a test issues completes within the test
	// body: there is no in-flight request left to race the assertion. Closing
	// first would only matter for a fake serving background traffic.
	t.Cleanup(func() { s.AssertDone(t) })

	return s
}

// baseURL is the address tests point the client at. The in-memory network
// routes every host to this server, so the name only has to be a valid URL
// that will never resolve for real.
const baseURL = "http://fake.gigahost.test"

// URL returns the base URL the client should point at. It is a fixed,
// unresolvable name: the in-memory transport ignores the host and delivers to
// this server regardless, but only when the request is issued through
// [Server.Client].
func (s *Server) URL() string { return baseURL }

// Client returns an [*http.Client] wired to this server's in-memory network.
// It is the only way to reach the fake; pass it to
// gigahost.WithHTTPClient alongside [Server.URL].
func (s *Server) Client() *http.Client { return s.server.Client() }

// Expect registers a new Expectation for the given method and path and
// returns a handle for configuring further matchers and the response.
//
// Path is matched exactly; include leading slash.
func (s *Server) Expect(method, path string) *Expectation {
	s.t.Helper()

	e := &Expectation{
		server:       s,
		method:       method,
		path:         path,
		responseCode: http.StatusOK,
		responseBody: []byte(`{"meta":{"status":200,"status_message":"200 OK"}}`),
	}

	s.mu.Lock()
	s.expectations = append(s.expectations, e)
	s.mu.Unlock()

	return e
}

// AssertDone fails the test if any registered expectations were not
// consumed. Called automatically at cleanup time but safe to call
// explicitly for finer-grained error reporting.
func (s *Server) AssertDone(t *testing.T) {
	t.Helper()

	s.mu.Lock()
	remaining := len(s.expectations) - s.index
	unmatched := s.unmatched
	s.mu.Unlock()

	if remaining > 0 {
		t.Errorf("testhelper: %d unmet expectation(s) remain", remaining)

		for i := s.index; i < len(s.expectations); i++ {
			t.Logf("  - %s %s", s.expectations[i].method, s.expectations[i].path)
		}
	}

	if unmatched > 0 {
		t.Errorf("testhelper: %d unexpected request(s) reached server", unmatched)
	}
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("testhelper: read request body: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	s.mu.Lock()

	if s.index >= len(s.expectations) {
		// Ordered script exhausted: let a Route answer if one matches.
		for _, route := range s.routes {
			if !route.matchesRoute(r) {
				continue
			}

			route.calls++
			call := route.calls
			s.mu.Unlock()
			s.writeResponse(w, route, r, call)

			return
		}

		s.unmatched++
		s.mu.Unlock()

		s.t.Errorf("testhelper: unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)

		return
	}

	e := s.expectations[s.index]
	s.index++
	e.calls++
	call := e.calls
	s.mu.Unlock()

	if err := e.match(r, body); err != nil {
		s.t.Errorf("testhelper: request mismatch at index %d: %v", s.index-1, err)
		http.Error(w, err.Error(), http.StatusTeapot)

		return
	}

	s.writeResponse(w, e, r, call)
}

// writeResponse emits the expectation's response, letting respondFn
// override the recorded status and body when one is set.
func (s *Server) writeResponse(w http.ResponseWriter, e *Expectation, r *http.Request, call int) {
	code, bodyOut := e.responseCode, e.responseBody

	if e.respondFn != nil {
		status, text := e.respondFn(r, call)
		code, bodyOut = status, []byte(text)
	}

	for k, v := range e.responseHeaders {
		w.Header().Set(k, v)
	}

	if _, ok := e.responseHeaders["Content-Type"]; !ok && looksLikeJSON(bodyOut) {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(code)
	_, _ = w.Write(bodyOut)
}

func looksLikeJSON(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if len(s) == 0 {
		return false
	}

	return s[0] == '{' || s[0] == '['
}
