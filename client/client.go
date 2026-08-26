package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Client is the entry point for the Gigahost API.
//
// A Client is safe for concurrent use by multiple goroutines once
// constructed. Every method takes a [context.Context] and returns an
// [*APIError] for non-2xx responses.
type Client struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string
	debugLog   func(string, ...any)

	// Authentication state. Exactly one of token or credentials is set
	// after NewClient returns (or the constructor errors). When
	// credentials are set, tokenMu guards token against races between
	// concurrent refresh attempts.
	tokenMu     sync.Mutex
	token       string
	credentials *credentials

	// Auth exposes the /authenticate endpoint and manages the
	// cached bearer token when the client was built with
	// [WithCredentials].
	Auth *AuthService

	// BGP exposes the /bgp endpoints for managing customer ASNs,
	// prefix lists and peering sessions.
	BGP *BGPService

	// DNS covers zones, records, redirects, DNSSEC, PTR zones,
	// domain registration and registrant management.
	DNS *DNSService

	// DynDNS provides the plain-text /dns/dyndns endpoint that
	// uses HTTP Basic authentication.
	DynDNS *DynDNSService

	// Deploy covers the /deploy endpoints for creating and monitoring
	// hourly-billed cloud VMs.
	Deploy *DeployService

	// Servers and its sibling services cover the /servers/*
	// endpoints: server data, power state, snapshots,
	// reinstall, KVM/IPMI sessions, ISO mounting and upgrades.
	Servers   *ServersService
	Snapshots *SnapshotsService
	Reinstall *ReinstallService
	IPMI      *IPMIService
	ISOs      *ISOsService
	Upgrades  *UpgradesService

	// Account and Invoices read the /my/* billing surface.
	Account  *AccountService
	Invoices *InvoicesService

	// Billing reads the /billing endpoint: prepaid credit and invoices.
	Billing *BillingService
}

// credentials holds username+password+optional-2fa state for automatic
// token acquisition and refresh.
type credentials struct {
	username string
	password string
	code     int
}

// NewClient constructs a client. At least one of [WithToken] or
// [WithCredentials] must be provided. Additional options may be used to
// customise the base URL, HTTP client, user-agent, logging, etc.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: DefaultUserAgent,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if c.token == "" && c.credentials == nil {
		return nil, errors.New("gigahost: NewClient: provide either WithToken or WithCredentials")
	}

	if c.httpClient == nil {
		// The default transport allows two idle connections per host. A
		// Terraform apply drives this single-host client at parallelism 10, so
		// most requests would pay a fresh TCP and TLS handshake and then throw
		// the connection away — measured at roughly two thirds of the time for
		// a small request.
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("gigahost: NewClient: unexpected default transport")
		}

		tr := transport.Clone()
		tr.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
		tr.MaxConnsPerHost = defaultMaxConnsPerHost

		c.httpClient = &http.Client{Timeout: DefaultHTTPTimeout, Transport: tr}
	}

	c.Auth = &AuthService{client: c}
	c.BGP = &BGPService{client: c}
	c.Deploy = &DeployService{client: c}
	c.DNS = &DNSService{client: c}
	c.DynDNS = &DynDNSService{client: c}
	c.Servers = &ServersService{client: c}
	c.Snapshots = &SnapshotsService{client: c}
	c.Reinstall = &ReinstallService{client: c}
	c.IPMI = &IPMIService{client: c}
	c.ISOs = &ISOsService{client: c}
	c.Upgrades = &UpgradesService{client: c}
	c.Account = &AccountService{client: c}
	c.Invoices = &InvoicesService{client: c}
	c.Billing = &BillingService{client: c}

	return c, nil
}

// BaseURL returns the URL this client makes requests against, including
// the /api/v0 path prefix when using the default.
func (c *Client) BaseURL() string { return c.baseURL }

// request is the internal workhorse. It:
//
//   - merges in the bearer token (unless skipAuth is true),
//   - marshals `body` as JSON when non-nil,
//   - attaches query parameters,
//   - decodes the response envelope if one is expected (dst non-nil),
//   - transparently refreshes the token on 401 when credentials are set.
//
// For endpoints that return non-JSON (like /dns/dyndns's plain-text
// response), pass `rawDst` to capture the raw bytes instead.
type requestOptions struct {
	method   string
	path     string
	query    url.Values
	body     any
	dst      any // envelope.Data is decoded into this, if non-nil
	rawDst   *[]byte
	skipAuth bool
	basic    *basicAuth
	// noEnvelope=true means the response body is not wrapped in the
	// standard {meta, data, success} envelope. The full body is
	// decoded into dst instead.
	noEnvelope bool
}

type basicAuth struct {
	username string
	password string
}

// do is the internal workhorse. It returns the parsed envelope meta
// (or a zero Meta when the endpoint uses a non-envelope response
// shape). The *Meta is part of the surface so service-layer code
// can surface API-provided status messages on success paths even
// when decode is into []byte via rawDst.
//
//nolint:unparam // callers currently ignore *Meta; kept for future use
func (c *Client) do(ctx context.Context, opts requestOptions) (*Meta, error) {
	if opts.method == "" {
		return nil, errors.New("gigahost: request: method is empty")
	}

	req, err := c.buildRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.roundTrip(req, opts)
	if err != nil {
		return nil, err
	}

	// Auto-refresh on 401 when credentials are set. We retry once.
	if resp.statusCode == http.StatusUnauthorized && c.credentials != nil && !opts.skipAuth {
		c.tokenMu.Lock()
		// Invalidate the current token so refresh is forced.
		c.token = ""
		c.tokenMu.Unlock()

		req2, err := c.buildRequest(ctx, opts)
		if err != nil {
			return nil, err
		}

		resp, err = c.roundTrip(req2, opts)
		if err != nil {
			return nil, err
		}
	}

	if resp.statusCode < 200 || resp.statusCode >= 300 {
		return nil, decodeAPIError(resp, opts)
	}

	return c.decodeBody(resp.body, opts)
}

// responseSummary is the fully-consumed result of a single HTTP round
// trip. The body has been read into memory and the response body has
// already been closed, so callers never need to worry about resource
// leakage. Keeping the raw *http.Response out of the public path also
// keeps the bodyclose linter happy.
type responseSummary struct {
	statusCode int
	status     string
	method     string
	url        string
	body       []byte
}

func (c *Client) buildRequest(ctx context.Context, opts requestOptions) (*http.Request, error) {
	u, err := c.url(opts.path, opts.query)
	if err != nil {
		return nil, err
	}

	var (
		reader  io.Reader
		rawBody []byte
	)

	if opts.body != nil {
		raw, err := marshalJSON(opts.body)
		if err != nil {
			return nil, fmt.Errorf("gigahost: marshal request body: %w", err)
		}

		rawBody = raw
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, opts.method, u, reader)
	if err != nil {
		return nil, fmt.Errorf("gigahost: build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	switch {
	case opts.basic != nil:
		req.SetBasicAuth(opts.basic.username, opts.basic.password)
	case !opts.skipAuth:
		tok, err := c.bearerToken(ctx)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+tok)
	}

	c.logRequest(req, rawBody)

	return req, nil
}

func (c *Client) roundTrip(req *http.Request, _ requestOptions) (*responseSummary, error) {
	// The URL we send is built from the configured baseURL plus a
	// fixed set of internal paths, so the gosec SSRF heuristic does
	// not apply here.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gigahost: http request: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gigahost: read response body: %w", err)
	}

	c.logResponse(req, resp, body)

	return &responseSummary{
		statusCode: resp.StatusCode,
		status:     resp.Status,
		method:     req.Method,
		url:        req.URL.String(),
		body:       body,
	}, nil
}

func (c *Client) decodeBody(body []byte, opts requestOptions) (*Meta, error) {
	if opts.rawDst != nil {
		*opts.rawDst = body

		return &Meta{}, nil
	}

	if opts.noEnvelope {
		if opts.dst != nil && len(body) > 0 {
			if err := unmarshalJSON(body, opts.dst); err != nil {
				return nil, fmt.Errorf("gigahost: decode response: %w", err)
			}
		}

		return &Meta{}, nil
	}

	if len(body) == 0 {
		return &Meta{}, nil
	}

	var env envelope
	if err := unmarshalJSON(body, &env); err != nil {
		return nil, fmt.Errorf("gigahost: decode envelope: %w", err)
	}

	meta := env.Meta

	if opts.dst != nil && len(env.Data) > 0 {
		if err := unmarshalJSON(env.Data, opts.dst); err != nil {
			return nil, fmt.Errorf("gigahost: decode data: %w", err)
		}
	}

	return &meta, nil
}

// bearerToken returns the current bearer token, delegating to the
// authentication service to fetch a fresh one if the client was built
// with [WithCredentials] and no token is currently cached.
func (c *Client) bearerToken(ctx context.Context) (string, error) {
	return c.ensureToken(ctx)
}

func (c *Client) url(path string, query url.Values) (string, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return "", fmt.Errorf("gigahost: parse URL %q: %w", c.baseURL+path, err)
	}

	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	return u.String(), nil
}

func decodeAPIError(resp *responseSummary, opts requestOptions) error {
	apiErr := &APIError{
		StatusCode: resp.statusCode,
		Status:     resp.status,
		Method:     opts.method,
		URL:        resp.url,
		RawBody:    resp.body,
	}

	if len(resp.body) > 0 {
		var env envelope
		if err := unmarshalJSON(resp.body, &env); err == nil {
			apiErr.Message = env.Meta.Message
			if apiErr.Message == "" {
				apiErr.Message = env.Meta.StatusMessage
			}
		}
	}

	return apiErr
}

// logRequest and logResponse emit structured log entries when a debug
// logger has been installed. Headers are redacted; bodies are truncated
// to avoid dumping secrets or bulk payloads into logs.
func (c *Client) logRequest(req *http.Request, body []byte) {
	if c.debugLog == nil {
		return
	}

	c.debugLog(
		"gigahost: HTTP request",
		"method", req.Method,
		"url", req.URL.String(),
		"body", truncateBody(body),
	)
}

func (c *Client) logResponse(req *http.Request, resp *http.Response, body []byte) {
	if c.debugLog == nil {
		return
	}

	c.debugLog(
		"gigahost: HTTP response",
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"duration", time.Duration(0), // populated by instrumented transport if user wants
		"body", truncateBody(body),
	)
}

const debugBodyLimit = 2 << 10 // 2 KiB

// secretJSONKeys are the fields whose values must never reach a log. The
// authenticate exchange carries a password, a 2FA code and a bearer token; a
// deploy returns the root password; an API key is returned once, in full.
var secretJSONKeys = []string{
	"password", "passwd", "root_passwd", "token", "secret", "api_key",
	"apikey", "key", "code", "kvm_password",
}

// redactSecrets blanks the value of any known-sensitive JSON key. It works on
// the raw bytes rather than by decoding, because a body that fails to decode
// is exactly the one worth logging — and must still be safe to log.
func redactSecrets(b []byte) []byte {
	for _, key := range secretJSONKeys {
		// "key" : "value"  →  "key":"[redacted]"
		re := regexp.MustCompile(`("` + regexp.QuoteMeta(key) + `"\s*:\s*)"[^"]*"`)
		b = re.ReplaceAll(b, []byte(`${1}"[redacted]"`))
	}

	return b
}

// truncateBody renders a body for the debug log: secrets removed first, then
// capped. Redaction happens before truncation so a secret cannot survive by
// sitting past the cut.
func truncateBody(b []byte) string {
	b = redactSecrets(b)

	if len(b) <= debugBodyLimit {
		return string(b)
	}

	return string(b[:debugBodyLimit]) + "…(truncated)"
}
