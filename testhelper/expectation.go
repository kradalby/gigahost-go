package testhelper

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"
)

// Expectation describes one expected request/response round-trip.
// Builders return *Expectation so callers can fluently chain matchers
// and the response.
type Expectation struct {
	server *Server

	method string
	path   string

	expectQuery   url.Values
	expectHeaders map[string]string
	expectJSON    []byte

	responseCode    int
	responseBody    []byte
	responseHeaders map[string]string
}

// WithQuery asserts that the query string contains (at least) the given
// key/value pairs. Unlisted keys are ignored.
func (e *Expectation) WithQuery(q url.Values) *Expectation {
	e.expectQuery = q

	return e
}

// WithQueryPairs is a convenience wrapper around WithQuery for simple
// "k1=v1, k2=v2" specifications.
func (e *Expectation) WithQueryPairs(pairs ...string) *Expectation {
	if len(pairs)%2 != 0 {
		panic("testhelper: WithQueryPairs: odd number of arguments")
	}

	q := url.Values{}

	for i := 0; i < len(pairs); i += 2 {
		q.Set(pairs[i], pairs[i+1])
	}

	return e.WithQuery(q)
}

// WithHeader asserts that the request includes the given header with
// the specified value.
func (e *Expectation) WithHeader(name, value string) *Expectation {
	if e.expectHeaders == nil {
		e.expectHeaders = map[string]string{}
	}

	e.expectHeaders[name] = value

	return e
}

// WithBearerToken asserts the Authorization header is exactly
// `Bearer <token>`.
func (e *Expectation) WithBearerToken(token string) *Expectation {
	return e.WithHeader("Authorization", "Bearer "+token)
}

// WithBasicAuth asserts the Authorization header corresponds to HTTP
// Basic authentication for the given credentials.
func (e *Expectation) WithBasicAuth(user, pass string) *Expectation {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.SetBasicAuth(user, pass)

	return e.WithHeader("Authorization", req.Header.Get("Authorization"))
}

// WithJSON asserts that the request body decodes to the same JSON value
// as the given string. Whitespace and field order are ignored.
func (e *Expectation) WithJSON(jsonStr string) *Expectation {
	e.expectJSON = []byte(jsonStr)

	return e
}

// Respond sets the HTTP status code and the response body verbatim.
func (e *Expectation) Respond(status int, body string) *Expectation {
	e.responseCode = status
	e.responseBody = []byte(body)

	return e
}

// RespondJSON sets the response body to the canonical JSON encoding of v
// and status to 200.
func (e *Expectation) RespondJSON(status int, v any) *Expectation {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("testhelper: marshal response: %v", err))
	}

	e.responseCode = status
	e.responseBody = b

	if e.responseHeaders == nil {
		e.responseHeaders = map[string]string{}
	}

	e.responseHeaders["Content-Type"] = "application/json"

	return e
}

// RespondFixture reads a fixture file and uses its contents as the
// response body. The status code is set to 200 unless overridden via
// Respond or RespondStatus.
func (e *Expectation) RespondFixture(t *testing.T, path string) *Expectation {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("testhelper: read fixture %q: %v", path, err)
	}

	e.responseCode = http.StatusOK
	e.responseBody = b

	return e
}

// RespondStatus sets only the status code, leaving the body unchanged.
func (e *Expectation) RespondStatus(code int) *Expectation {
	e.responseCode = code

	return e
}

// RespondHeader adds a header to the response.
func (e *Expectation) RespondHeader(name, value string) *Expectation {
	if e.responseHeaders == nil {
		e.responseHeaders = map[string]string{}
	}

	e.responseHeaders[name] = value

	return e
}

// match verifies the incoming request against this expectation and
// returns a descriptive error when any check fails.
func (e *Expectation) match(r *http.Request, body []byte) error {
	if r.Method != e.method {
		return fmt.Errorf("method: want %s, got %s", e.method, r.Method)
	}

	if r.URL.Path != e.path {
		return fmt.Errorf("path: want %s, got %s", e.path, r.URL.Path)
	}

	for k, want := range e.expectHeaders {
		if got := r.Header.Get(k); got != want {
			return fmt.Errorf("header %s: want %q, got %q", k, want, got)
		}
	}

	if len(e.expectQuery) > 0 {
		got := r.URL.Query()

		for k, wantValues := range e.expectQuery {
			gotValues := got[k]
			if !equalStringSlice(gotValues, wantValues) {
				return fmt.Errorf("query %s: want %v, got %v", k, wantValues, gotValues)
			}
		}
	}

	if len(e.expectJSON) > 0 {
		var wantAny, gotAny any
		if err := json.Unmarshal(e.expectJSON, &wantAny); err != nil {
			return fmt.Errorf("expected-json malformed: %w", err)
		}

		if err := json.Unmarshal(body, &gotAny); err != nil {
			return fmt.Errorf("request body not JSON: %w (body=%s)", err, truncate(body))
		}

		if !deepEqualJSON(wantAny, gotAny) {
			wantBytes, _ := json.Marshal(wantAny)
			gotBytes, _ := json.Marshal(gotAny)

			return fmt.Errorf("body mismatch:\n  want=%s\n  got =%s", wantBytes, gotBytes)
		}
	}

	return nil
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	sort.Strings(a)
	sort.Strings(b)

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// deepEqualJSON compares two decoded JSON values for structural equality
// ignoring map key order.
func deepEqualJSON(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return false
		}

		if len(av) != len(bv) {
			return false
		}

		for k, va := range av {
			vb, ok := bv[k]
			if !ok || !deepEqualJSON(va, vb) {
				return false
			}
		}

		return true
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return false
		}

		if len(av) != len(bv) {
			return false
		}

		for i := range av {
			if !deepEqualJSON(av[i], bv[i]) {
				return false
			}
		}

		return true
	default:
		return a == b
	}
}

func truncate(b []byte) string {
	const maxLen = 512
	if len(b) <= maxLen {
		return string(b)
	}

	return string(b[:maxLen]) + "...(truncated)"
}

// ReadFixture reads a fixture file into a byte slice, failing the test
// on error. It mirrors the style of Go's standard test helpers.
func ReadFixture(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("testhelper: read fixture %q: %v", path, err)
	}

	return b
}

// UnmarshalFixture is a convenience for tests that need to compare the
// decoded contents of a fixture with the client's output.
func UnmarshalFixture(t *testing.T, path string, v any) {
	t.Helper()

	b := ReadFixture(t, path)
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("testhelper: unmarshal fixture %q: %v", path, err)
	}
}

// JSONString returns v as a compact JSON string, panicking on error.
// Useful in one-line RespondJSON calls.
func JSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes.TrimSpace(b))
}

// TrimPrefix returns s with prefix stripped, unchanged if absent. Small
// helper used by a couple of test files.
func TrimPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }
