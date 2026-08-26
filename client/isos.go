package client

import (
	"context"
	"errors"
	"net/url"
)

// ISOsService handles /servers/{id}/isos.
type ISOsService struct {
	client *Client
}

// ISO represents an uploaded ISO image that can be mounted to a server.
type ISO struct {
	ID         string
	CustomerID string
	URL        string
	Name       string
	Hash       string
	Size       int64
	State      string
	Mounted    bool
}

// UnmarshalJSON maps snake_case fields and numeric/boolean strings.
func (i *ISO) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID         string  `json:"iso_id"`
		CustomerID string  `json:"cust_id"`
		URL        string  `json:"iso_url"`
		Name       string  `json:"iso_name"`
		Hash       string  `json:"iso_hash"`
		Size       apiInt  `json:"iso_size"`
		State      string  `json:"iso_state"`
		Mounted    apiBool `json:"iso_mounted"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*i = ISO{
		ID:         r.ID,
		CustomerID: r.CustomerID,
		URL:        r.URL,
		Name:       r.Name,
		Hash:       r.Hash,
		Size:       int64(r.Size),
		State:      r.State,
		Mounted:    bool(r.Mounted),
	}

	return nil
}

// List returns the list of uploaded ISOs.
func (s *ISOsService) List(ctx context.Context, serverID string) ([]ISO, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: ISOs.List: serverID is empty")
	}

	var out []ISO
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID) + "/isos",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// Mount mounts the identified ISO on the server.
func (s *ISOsService) Mount(ctx context.Context, serverID, isoID string) error {
	if serverID == "" || isoID == "" {
		return errors.New("gigahost: ISOs.Mount: serverID and isoID are required")
	}

	body := map[string]string{"iso_id": isoID}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + url.PathEscape(serverID) + "/isos",
		body:   body,
	})

	return err
}
