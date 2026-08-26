package client

import (
	"errors"
	"net/http"
	"time"
)

// DefaultBaseURL is the production Gigahost API base URL.
const DefaultBaseURL = "https://api.gigahost.no/api/v0"

// Connection pool sizing. Terraform's default parallelism is 10; leaving
// headroom above it means a burst does not fall back to fresh handshakes.
const (
	defaultMaxIdleConnsPerHost = 20
	defaultMaxConnsPerHost     = 20
)

// DefaultUserAgent is sent when the caller does not override it.
const DefaultUserAgent = "gigahost-go/0.1 (+https://github.com/kradalby/gigahost-go)"

// DefaultHTTPTimeout is applied to the default HTTP client when the caller
// does not provide one.
const DefaultHTTPTimeout = 60 * time.Second

// Option configures a [Client] at construction time. Options are applied
// in the order passed to [NewClient]; later options override earlier ones.
type Option func(*Client) error

// WithToken sets a bearer token used on every request. Use this when the
// token has already been obtained out-of-band (for example, from the
// result of POST /authenticate).
func WithToken(token string) Option {
	return func(c *Client) error {
		if token == "" {
			return errors.New("gigahost: WithToken: token is empty")
		}

		c.token = token

		return nil
	}
}

// WithCredentials configures the client with username+password. The
// client will lazily call POST /authenticate on the first request that
// needs a token and will attempt one automatic token refresh on 401.
//
// `code` is the 2FA/TOTP code and may be zero when not required.
func WithCredentials(username, password string, code int) Option {
	return func(c *Client) error {
		if username == "" || password == "" {
			return errors.New("gigahost: WithCredentials: username and password are required")
		}

		c.credentials = &credentials{
			username: username,
			password: password,
			code:     code,
		}

		return nil
	}
}

// WithBaseURL overrides the API base URL. The URL must not end with a
// trailing slash.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) error {
		if baseURL == "" {
			return errors.New("gigahost: WithBaseURL: baseURL is empty")
		}

		c.baseURL = baseURL

		return nil
	}
}

// WithHTTPClient lets the caller supply an [*http.Client], typically to
// configure timeouts, transport-level retries, tracing or proxy support.
//
// If the provided client's Timeout is zero, it will be left untouched;
// the caller is expected to set their own.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) error {
		if httpClient == nil {
			return errors.New("gigahost: WithHTTPClient: httpClient is nil")
		}

		c.httpClient = httpClient

		return nil
	}
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) error {
		if ua == "" {
			return errors.New("gigahost: WithUserAgent: user-agent is empty")
		}

		c.userAgent = ua

		return nil
	}
}

// WithDebugLogger installs a structured logger that receives one entry
// per HTTP request and response. The logger is called with a plain
// message and alternating key-value pairs, in the same style as slog.
//
// Headers are not logged at all, and bodies are truncated to 2KiB with
// known-sensitive fields — passwords, tokens, API-key secrets, root
// passwords, 2FA codes — replaced by "[redacted]". A nil logger disables
// debug logging.
//
// Treat the output as sensitive regardless: a field this package does not
// know about cannot be redacted.
func WithDebugLogger(logger func(msg string, keysAndValues ...any)) Option {
	return func(c *Client) error {
		c.debugLog = logger

		return nil
	}
}
