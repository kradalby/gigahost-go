package client

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// PowerState is the response from /servers/{id}/powerstate.
//
// The API puts `powerstate` and `timestamp` at the top level of the
// response body rather than inside `data`, so we decode without
// envelope stripping.
type PowerState struct {
	PowerState bool
	Timestamp  time.Time
}

// UnmarshalJSON handles the flat structure and unix timestamp.
func (p *PowerState) UnmarshalJSON(data []byte) error {
	type raw struct {
		Success    bool        `json:"success"`
		Meta       Meta        `json:"meta"`
		PowerState bool        `json:"powerstate"`
		Timestamp  apiUnixTime `json:"timestamp"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	p.PowerState = r.PowerState
	p.Timestamp = time.Time(r.Timestamp)

	return nil
}

// GetPowerState returns whether the server is powered on.
func (s *ServersService) GetPowerState(ctx context.Context, serverID string) (*PowerState, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: GetPowerState: serverID is empty")
	}

	var out PowerState
	if _, err := s.client.do(ctx, requestOptions{
		method:     "GET",
		path:       "/servers/" + url.PathEscape(serverID) + "/powerstate",
		dst:        &out,
		noEnvelope: true,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// Reboot triggers a reboot of the server.
func (s *ServersService) Reboot(ctx context.Context, serverID string) error {
	if serverID == "" {
		return errors.New("gigahost: Reboot: serverID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID) + "/reboot",
	})

	return err
}

// PowerOn turns the server on.
func (s *ServersService) PowerOn(ctx context.Context, serverID string) error {
	if serverID == "" {
		return errors.New("gigahost: PowerOn: serverID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID) + "/power/on",
	})

	return err
}

// PowerOff turns the server off.
func (s *ServersService) PowerOff(ctx context.Context, serverID string) error {
	if serverID == "" {
		return errors.New("gigahost: PowerOff: serverID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID) + "/power/off",
	})

	return err
}
