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
	server       *httptest.Server
	index        int
	unmatched    int
}

// NewServer constructs a Server registered for cleanup with t.
func NewServer(t *testing.T) *Server {
	t.Helper()

	s := &Server{t: t}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))

	t.Cleanup(func() {
		s.server.Close()
		s.AssertDone(t)
	})

	return s
}

// URL returns the base URL the client should point at.
func (s *Server) URL() string { return s.server.URL }

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
		s.unmatched++
		s.mu.Unlock()

		s.t.Errorf("testhelper: unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", http.StatusTeapot)

		return
	}

	e := s.expectations[s.index]
	s.index++
	s.mu.Unlock()

	if err := e.match(r, body); err != nil {
		s.t.Errorf("testhelper: request mismatch at index %d: %v", s.index-1, err)
		http.Error(w, err.Error(), http.StatusTeapot)

		return
	}

	for k, v := range e.responseHeaders {
		w.Header().Set(k, v)
	}

	if _, ok := e.responseHeaders["Content-Type"]; !ok && looksLikeJSON(e.responseBody) {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(e.responseCode)
	_, _ = w.Write(e.responseBody)
}

func looksLikeJSON(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if len(s) == 0 {
		return false
	}

	return s[0] == '{' || s[0] == '['
}
