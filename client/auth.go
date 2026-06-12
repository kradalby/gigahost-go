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

// Authenticate calls POST /authenticate and returns a new [Token]. When
// req is nil, the credentials configured on the client (via
// [WithCredentials]) are used.
//
// On success the token is also stored on the client for subsequent
// authenticated requests.
func (s *AuthService) Authenticate(ctx context.Context, req *AuthenticateRequest) (*Token, error) {
	if req == nil {
		if s.client.credentials == nil {
			return nil, errors.New("gigahost: Authenticate: no credentials provided and none configured on client")
		}

		req = &AuthenticateRequest{
			Username: s.client.credentials.username,
			Password: s.client.credentials.password,
			Code:     s.client.credentials.code,
		}
	}

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

	s.client.tokenMu.Lock()
	s.client.token = tok.Token
	s.client.tokenMu.Unlock()

	return &tok, nil
}

// ensureToken returns a valid bearer token, calling Authenticate on
// demand when credentials are configured but no token has been fetched
// yet. It is used internally by the request pipeline.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	tok := c.token
	c.tokenMu.Unlock()

	if tok != "" {
		return tok, nil
	}

	if c.credentials == nil {
		return "", errors.New("gigahost: no token available and no credentials configured")
	}

	if _, err := c.Auth.Authenticate(ctx, nil); err != nil {
		return "", fmt.Errorf("gigahost: auto-authenticate: %w", err)
	}

	c.tokenMu.Lock()
	tok = c.token
	c.tokenMu.Unlock()

	return tok, nil
}
