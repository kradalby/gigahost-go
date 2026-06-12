package client

import (
	"context"
	"errors"
	"fmt"
)

// UserServer is a server entry in a user contact's assigned-server list.
type UserServer struct {
	RelationID string // id field (the assignment relation, used for revocation)
	ServerID   string
	ServerName string
	IPAddress  string
}

// UnmarshalJSON maps snake_case API fields.
func (u *UserServer) UnmarshalJSON(data []byte) error {
	type raw struct {
		RelationID string `json:"id"`
		ServerID   string `json:"srv_id"`
		ServerName string `json:"srv_name"`
		IPAddress  string `json:"ip_address"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*u = UserServer(r)

	return nil
}

// UserServerUnassigned is an entry in the servers_unassigned list returned
// by GET /account/user/{id}. It has no relation ID.
type UserServerUnassigned struct {
	ServerID   string `json:"srv_id"`
	ServerName string `json:"srv_name"`
	IPAddress  string `json:"ip_address"`
}

// UserDetails is the full response from GET /account/user/{id}.
type UserDetails struct {
	ID          string
	Name        string
	Username    string
	AccessLevel string
	Active      bool
	TwoFA       bool

	Servers           []UserServer
	ServersUnassigned []UserServerUnassigned
}

// UnmarshalJSON maps snake_case API fields.
func (u *UserDetails) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          string                 `json:"contact_id"`
		Name        string                 `json:"contact_name"`
		Username    string                 `json:"contact_username"`
		AccessLevel string                 `json:"contact_access_level"`
		Active      apiBool                `json:"contact_active"`
		TwoFA       apiBool                `json:"contact_2fa"`
		Servers     []UserServer           `json:"servers"`
		Unassigned  []UserServerUnassigned `json:"servers_unassigned"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*u = UserDetails{
		ID:                r.ID,
		Name:              r.Name,
		Username:          r.Username,
		AccessLevel:       r.AccessLevel,
		Active:            bool(r.Active),
		TwoFA:             bool(r.TwoFA),
		Servers:           r.Servers,
		ServersUnassigned: r.Unassigned,
	}

	return nil
}

// InviteUserRequest is the body for POST /account (invite a new user contact).
type InviteUserRequest struct {
	Name        string `json:"name"`
	Username    string `json:"username"`    // email address
	AccessLevel string `json:"accesslevel"` // "admin", "user", or "server"
}

// UpdateUserRequest is the body for PUT /account/user/{id}.
type UpdateUserRequest struct {
	Name        string `json:"contact_name"`
	Username    string `json:"contact_username"`          // email address
	AccessLevel string `json:"contact_access_level"`      // "admin", "user", or "server"
	Password    string `json:"contact_password,omitzero"` // optional
}

// GetUser returns the full details for one user contact including
// assigned and available servers.
func (s *AccountService) GetUser(ctx context.Context, userID string) (*UserDetails, error) {
	if userID == "" {
		return nil, errors.New("gigahost: GetUser: userID is required")
	}

	var out UserDetails
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/account/user/" + userID,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// InviteUser creates a new user contact. The API sends a password-setup
// link to the new contact's email address.
func (s *AccountService) InviteUser(ctx context.Context, req InviteUserRequest) error {
	if req.Name == "" {
		return errors.New("gigahost: InviteUser: Name is required")
	}

	if req.Username == "" {
		return errors.New("gigahost: InviteUser: Username (email) is required")
	}

	if req.AccessLevel == "" {
		return errors.New("gigahost: InviteUser: AccessLevel is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/account",
		body:   req,
	})

	return err
}

// UpdateUser updates an existing user contact.
func (s *AccountService) UpdateUser(ctx context.Context, userID string, req UpdateUserRequest) error {
	if userID == "" {
		return errors.New("gigahost: UpdateUser: userID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/account/user/" + userID,
		body:   req,
	})

	return err
}

// DeleteUser removes a user contact from the account.
func (s *AccountService) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("gigahost: DeleteUser: userID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/account/user/" + userID,
	})

	return err
}

// GrantServerAccess assigns a server to a user contact.
func (s *AccountService) GrantServerAccess(ctx context.Context, userID, serverID string) error {
	if userID == "" {
		return errors.New("gigahost: GrantServerAccess: userID is required")
	}

	if serverID == "" {
		return errors.New("gigahost: GrantServerAccess: serverID is required")
	}

	body := map[string]string{"srv_id": serverID}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   fmt.Sprintf("/account/user/%s/server", userID),
		body:   body,
	})

	return err
}

// RevokeServerAccess removes a server assignment from a user contact.
// relationID comes from UserServer.RelationID (the "id" field in the
// servers list).
func (s *AccountService) RevokeServerAccess(ctx context.Context, userID, relationID string) error {
	if userID == "" {
		return errors.New("gigahost: RevokeServerAccess: userID is required")
	}

	if relationID == "" {
		return errors.New("gigahost: RevokeServerAccess: relationID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   fmt.Sprintf("/account/user/%s/server/%s", userID, relationID),
	})

	return err
}
