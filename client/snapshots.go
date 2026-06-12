package client

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// SnapshotsService groups snapshot endpoints under /servers/{id}.
type SnapshotsService struct {
	client *Client
}

// SnapshotState enumerates snapshot lifecycle states.
type SnapshotState string

const (
	SnapshotStatePending   SnapshotState = "pending"
	SnapshotStateCompleted SnapshotState = "completed"
)

// Snapshot represents a saved point-in-time image of a VPS.
type Snapshot struct {
	ID          int64
	ServerID    int64
	Name        string
	DisplayName string
	Time        time.Time
	State       SnapshotState
}

// UnmarshalJSON maps the API's snap_* fields.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          apiInt        `json:"snap_id"`
		ServerID    apiInt        `json:"srv_id"`
		Name        string        `json:"snap_name"`
		DisplayName string        `json:"snap_display_name"`
		Time        apiUnixTime   `json:"snap_time"`
		State       SnapshotState `json:"snap_state"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*s = Snapshot{
		ID:          int64(r.ID),
		ServerID:    int64(r.ServerID),
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Time:        time.Time(r.Time),
		State:       r.State,
	}

	return nil
}

// List returns all snapshots for a server.
func (s *SnapshotsService) List(ctx context.Context, serverID string) ([]Snapshot, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: Snapshots.List: serverID is empty")
	}

	var out []Snapshot
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + serverID + "/snapshots",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// Create asks the server to create a snapshot with the given name.
// Snapshot creation is asynchronous; poll [List] for the final state.
func (s *SnapshotsService) Create(ctx context.Context, serverID, name string) error {
	if serverID == "" {
		return errors.New("gigahost: Snapshots.Create: serverID is empty")
	}

	if name == "" {
		return errors.New("gigahost: Snapshots.Create: name is required")
	}

	body := map[string]string{"name": name}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + serverID + "/snapshot",
		body:   body,
	})

	return err
}

// Delete deletes a snapshot by ID.
func (s *SnapshotsService) Delete(ctx context.Context, serverID string, snapshotID int64) error {
	if serverID == "" {
		return errors.New("gigahost: Snapshots.Delete: serverID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/servers/" + serverID + "/snapshot/" + strconv.FormatInt(snapshotID, 10),
	})

	return err
}
