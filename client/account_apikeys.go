package client

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// APIKeyPermission is the access rule for one category within an API key.
type APIKeyPermission struct {
	Mode string   // "r" or "rw"
	All  bool     // true = all resource IDs; false = restricted to IDs list
	IDs  []string // only used when All is false
}

// UnmarshalJSON maps the permission object.
func (p *APIKeyPermission) UnmarshalJSON(data []byte) error {
	type raw struct {
		Mode string   `json:"mode"`
		All  bool     `json:"all"`
		IDs  []string `json:"ids"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*p = APIKeyPermission(r)

	return nil
}

// MarshalJSON encodes the permission object.
func (p APIKeyPermission) MarshalJSON() ([]byte, error) {
	type wire struct {
		Mode string   `json:"mode"`
		All  bool     `json:"all"`
		IDs  []string `json:"ids,omitzero"`
	}

	return marshalJSON(wire(p))
}

// APIKeyPermissions holds per-category permission rules for an API key.
// Nil fields indicate no access is granted for that category.
type APIKeyPermissions struct {
	DNS        *APIKeyPermission `json:"dns,omitzero"`
	Servers    *APIKeyPermission `json:"servers,omitzero"`
	Webhosting *APIKeyPermission `json:"webhosting,omitzero"`
	Racks      *APIKeyPermission `json:"racks,omitzero"`
	Support    *APIKeyPermission `json:"support,omitzero"`
	Billing    *APIKeyPermission `json:"billing,omitzero"`
	Account    *APIKeyPermission `json:"account,omitzero"`
}

// APIKey is one personal API key as returned by the list/get endpoints.
// The secret value is never included; only the key_prefix is safe to display.
type APIKey struct {
	ID          string
	Label       string
	Prefix      string
	Permissions APIKeyPermissions
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	LastUsedIP  string
	Status      string
	RevokedAt   *time.Time
	ContactID   string
}

// UnmarshalJSON maps snake_case API fields.
func (k *APIKey) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          string            `json:"key_id"`
		Label       string            `json:"key_label"`
		Prefix      string            `json:"key_prefix"`
		Permissions APIKeyPermissions `json:"permissions"`
		CreatedAt   apiUnixTime       `json:"created_at"`
		ExpiresAt   *apiUnixTime      `json:"expires_at"`
		LastUsedAt  *apiUnixTime      `json:"last_used_at"`
		LastUsedIP  string            `json:"last_used_ip"`
		Status      string            `json:"status"`
		RevokedAt   *apiUnixTime      `json:"revoked_at"`
		ContactID   string            `json:"contact_id"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*k = APIKey{
		ID:          r.ID,
		Label:       r.Label,
		Prefix:      r.Prefix,
		Permissions: r.Permissions,
		CreatedAt:   time.Time(r.CreatedAt),
		LastUsedIP:  r.LastUsedIP,
		Status:      r.Status,
		ContactID:   r.ContactID,
	}

	if r.ExpiresAt != nil {
		t := time.Time(*r.ExpiresAt)
		k.ExpiresAt = &t
	}

	if r.LastUsedAt != nil {
		t := time.Time(*r.LastUsedAt)
		k.LastUsedAt = &t
	}

	if r.RevokedAt != nil {
		t := time.Time(*r.RevokedAt)
		k.RevokedAt = &t
	}

	return nil
}

// CreateAPIKeyRequest is the body for POST /account/apikeys.
type CreateAPIKeyRequest struct {
	Label       string            `json:"label"`
	Permissions APIKeyPermissions `json:"permissions"`
	ExpiresAt   *int64            `json:"expires_at,omitzero"` // Unix timestamp; nil = no expiry
}

// UpdateAPIKeyRequest is the body for PUT /account/apikeys/{id}.
// At least one of Label or Permissions must be non-zero.
type UpdateAPIKeyRequest struct {
	Label       string             `json:"label,omitzero"`
	Permissions *APIKeyPermissions `json:"permissions,omitzero"`
}

// APIKeyCreated is the response from POST /account/apikeys and
// POST /account/apikeys/{id}/rotate. The Secret field is shown only
// once and cannot be recovered later.
type APIKeyCreated struct {
	Secret      string
	Prefix      string
	Label       string
	ExpiresAt   *time.Time
	Permissions APIKeyPermissions
}

// UnmarshalJSON maps the create/rotate response.
func (a *APIKeyCreated) UnmarshalJSON(data []byte) error {
	type raw struct {
		Secret      string            `json:"secret"`
		Prefix      string            `json:"prefix"`
		Label       string            `json:"label"`
		ExpiresAt   *apiUnixTime      `json:"expires_at"`
		Permissions APIKeyPermissions `json:"permissions"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*a = APIKeyCreated{
		Secret:      r.Secret,
		Prefix:      r.Prefix,
		Label:       r.Label,
		Permissions: r.Permissions,
	}

	if r.ExpiresAt != nil {
		t := time.Time(*r.ExpiresAt)
		a.ExpiresAt = &t
	}

	return nil
}

// APIKeyRotated is the response from POST /account/apikeys/{id}/rotate.
// It contains only the new secret and its prefix.
type APIKeyRotated struct {
	Secret string `json:"secret"`
	Prefix string `json:"prefix"`
}

// ListAPIKeys returns all active API keys for the account.
// Secrets are never returned; only the key_prefix is included.
func (s *AccountService) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	var out []APIKey
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/account/apikeys",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// GetAPIKey returns one API key by its numeric ID.
func (s *AccountService) GetAPIKey(ctx context.Context, keyID string) (*APIKey, error) {
	if keyID == "" {
		return nil, errors.New("gigahost: GetAPIKey: keyID is required")
	}

	var out APIKey
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/account/apikeys/" + keyID,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// CreateAPIKey creates a new personal API key. The returned secret is
// shown only once; store it immediately.
func (s *AccountService) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (*APIKeyCreated, error) {
	if req.Label == "" {
		return nil, errors.New("gigahost: CreateAPIKey: Label is required")
	}

	var out APIKeyCreated
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/account/apikeys",
		body:   req,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// UpdateAPIKey updates the label and/or permissions of an existing API key.
func (s *AccountService) UpdateAPIKey(ctx context.Context, keyID string, req UpdateAPIKeyRequest) error {
	if keyID == "" {
		return errors.New("gigahost: UpdateAPIKey: keyID is required")
	}

	if req.Label == "" && req.Permissions == nil {
		return errors.New("gigahost: UpdateAPIKey: at least one of Label or Permissions must be set")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/account/apikeys/" + keyID,
		body:   req,
	})

	return err
}

// DeleteAPIKey revokes an API key immediately.
func (s *AccountService) DeleteAPIKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return errors.New("gigahost: DeleteAPIKey: keyID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/account/apikeys/" + keyID,
	})

	return err
}

// RotateAPIKey rotates the secret of an existing API key. The previous
// secret stops working immediately. The new secret is returned once;
// store it immediately.
func (s *AccountService) RotateAPIKey(ctx context.Context, keyID string) (*APIKeyRotated, error) {
	if keyID == "" {
		return nil, errors.New("gigahost: RotateAPIKey: keyID is required")
	}

	var out APIKeyRotated
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   fmt.Sprintf("/account/apikeys/%s/rotate", keyID),
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
