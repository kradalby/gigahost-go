package client

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors used for [errors.Is] matching. APIError implements Is()
// so callers can write:
//
//	if errors.Is(err, client.ErrNotFound) { /* ... */ }
var (
	ErrUnauthorized = errors.New("gigahost: unauthorized")
	ErrForbidden    = errors.New("gigahost: forbidden")
	ErrNotFound     = errors.New("gigahost: not found")
	ErrConflict     = errors.New("gigahost: conflict")
	ErrRateLimited  = errors.New("gigahost: rate limited")
	ErrServer       = errors.New("gigahost: server error")
)

// APIError is returned for any non-2xx response from the Gigahost API.
//
// The zero value is not useful; construct via the client's internal
// request path.
type APIError struct {
	// StatusCode is the HTTP status code (e.g. 404).
	StatusCode int
	// Status is the full HTTP status line (e.g. "404 Not Found").
	Status string
	// Method is the HTTP method of the failing request.
	Method string
	// URL is the absolute URL of the failing request (with credentials
	// redacted by the caller).
	URL string
	// Message is the value of meta.message from the response body, if
	// the body parsed as a Gigahost envelope. Empty otherwise.
	Message string
	// RawBody is the full response body for diagnostics. Do not parse
	// programmatically; use Message and status fields instead.
	RawBody []byte
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("gigahost: %s %s: %d %s: %s", e.Method, e.URL, e.StatusCode, e.Status, e.Message)
	}

	return fmt.Sprintf("gigahost: %s %s: %d %s", e.Method, e.URL, e.StatusCode, e.Status)
}

// IsNotFound reports whether err is a definitive not-found response from
// the Gigahost API (HTTP 404). Resource Read handlers should call
// RemoveResource only when this returns true; any other error is transient
// and must be surfaced to the caller.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// Is enables errors.Is matching against the sentinels above and against
// generic http.StatusXxx values.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= 500 && e.StatusCode < 600
	}

	return false
}
