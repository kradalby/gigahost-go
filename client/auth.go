package client

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AuthService handles API authentication. The service lives on every
// Client so callers can re-authenticate explicitly; ordinarily this is
// driven automatically by the client based on the constructor options.
type AuthService struct {
	client *Client
}

// Token is the result of successful authentication.
type Token struct {
	// Token is the bearer token used in the Authorization header.
	Token string `json:"token"`
	// ExpiresAt is when the token expires.
	ExpiresAt time.Time `json:"expires_at"`
	// CustomerID is the account the token belongs to.
	CustomerID string `json:"customer_id"`
}

// UnmarshalJSON normalises the API representation (unix timestamp as a
// string) into a proper [time.Time].
func (t *Token) UnmarshalJSON(data []byte) error {
	type raw struct {
		Token       string      `json:"token"`
		TokenExpire apiUnixTime `json:"token_expire"`
		CustomerID  string      `json:"customer_id"`
	}

	var r raw

	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	t.Token = r.Token
	t.ExpiresAt = time.Time(r.TokenExpire)
	t.CustomerID = r.CustomerID

	return nil
}

// AuthenticateRequest is the POST body for /authenticate.
type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// Code is the 2FA code. Zero means "not provided".
	Code int `json:"code,omitzero"`
}

// Authenticate calls POST /authenticate and returns a new [Token].
//
// When req is nil, the credentials configured on the client (via
// [WithCredentials]) are used, and the client adopts the token for
// subsequent requests. Other credentials only yield their token: the client
// is shared, and adopting it would switch every other caller to that user.
func (s *AuthService) Authenticate(ctx context.Context, req *AuthenticateRequest) (*Token, error) {
	if req != nil {
		return s.login(ctx, *req)
	}

	if s.client.credentials == nil {
		return nil, errors.New("gigahost: Authenticate: no credentials provided and none configured on client")
	}

	tok, err := s.login(ctx, s.client.credentials.request())
	if err != nil {
		return nil, err
	}

	s.client.tokenMu.Lock()
	s.client.token = tok.Token
	s.client.tokenMu.Unlock()

	return tok, nil
}

// login exchanges credentials for a token without touching client state.
func (s *AuthService) login(ctx context.Context, req AuthenticateRequest) (*Token, error) {
	if req.Username == "" || req.Password == "" {
		return nil, errors.New("gigahost: Authenticate: username and password are required")
	}

	var tok Token

	if _, err := s.client.do(ctx, requestOptions{
		method:   "POST",
		path:     "/authenticate",
		body:     req,
		dst:      &tok,
		skipAuth: true,
	}); err != nil {
		return nil, err
	}

	if tok.Token == "" {
		return nil, errors.New("gigahost: Authenticate: API returned empty token")
	}

	return &tok, nil
}

// tokenRefresh is one in-flight /authenticate that parallel callers share.
type tokenRefresh struct {
	done  chan struct{}
	token string
	err   error
	// abandoned marks a login whose own caller gave up; its error says
	// nothing about the credentials, so waiters try again.
	abandoned bool
}

// ensureToken returns the bearer token, logging in first when the client
// has credentials but no token.
//
// Parallel callers share one login rather than each making their own: that
// is one request instead of ten under Terraform, and if the API keeps one
// session per user, every extra login would revoke the token another caller
// was just handed.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	for {
		c.tokenMu.Lock()

		if tok := c.token; tok != "" {
			c.tokenMu.Unlock()

			return tok, nil
		}

		if c.credentials == nil {
			c.tokenMu.Unlock()

			return "", errors.New("gigahost: no token available and no credentials configured")
		}

		if r := c.refresh; r != nil {
			c.tokenMu.Unlock()

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-r.done:
			}

			if r.abandoned {
				continue
			}

			return r.token, r.err
		}

		r := &tokenRefresh{done: make(chan struct{})}
		c.refresh = r
		c.tokenMu.Unlock()

		tok, err := c.Auth.login(ctx, c.credentials.request())

		c.tokenMu.Lock()
		c.refresh = nil

		if err != nil {
			r.err = fmt.Errorf("gigahost: auto-authenticate: %w", err)
			r.abandoned = ctx.Err() != nil
		} else {
			r.token = tok.Token
			c.token = tok.Token
		}

		c.tokenMu.Unlock()
		close(r.done)

		return r.token, r.err
	}
}

// discardToken forgets tok after the API refused it. A parallel caller may
// already have replaced it; clearing that fresh token would force a second
// login for nothing.
func (c *Client) discardToken(tok string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token == tok {
		c.token = ""
	}
}
