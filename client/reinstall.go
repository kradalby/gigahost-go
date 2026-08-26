package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// ReinstallService handles OS reinstallation endpoints.
type ReinstallService struct {
	client *Client

	// allOSes memoises the flattened operating-system list. Building it costs
	// one request per distribution — ten against the live API — and every
	// server refresh used to rebuild it.
	allOSes cached[[]ResolvedOS]
}

// Distribution describes an available OS distribution for reinstall.
type Distribution struct {
	ID          string
	TypeID      string
	Name        string
	Value       string
	Logo        string
	Description string
	Active      bool
}

// UnmarshalJSON maps snake_case fields.
func (d *Distribution) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          string  `json:"dist_id"`
		TypeID      string  `json:"type_id"`
		Name        string  `json:"dist_name"`
		Value       string  `json:"dist_value"`
		Logo        string  `json:"dist_logo"`
		Description string  `json:"dist_description"`
		Active      apiBool `json:"dist_active"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*d = Distribution{
		ID:          r.ID,
		TypeID:      r.TypeID,
		Name:        r.Name,
		Value:       r.Value,
		Logo:        r.Logo,
		Description: r.Description,
		Active:      bool(r.Active),
	}

	return nil
}

// ReinstallOS describes one OS available for reinstall.
type ReinstallOS struct {
	ID              string
	DistributionID  string
	Name            string
	Release         string
	Distribution    string
	Arch            string
	CustomPartition bool
	SingleDiskOnly  bool
	SupportRAID     bool
	DedicatedOnly   bool
	MinRAM          int
}

// UnmarshalJSON maps snake_case fields.
func (o *ReinstallOS) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID              string  `json:"os_id"`
		DistributionID  string  `json:"dist_id"`
		Name            string  `json:"os_name"`
		Release         string  `json:"os_release"`
		Distribution    string  `json:"os_dist"`
		Arch            string  `json:"os_arch"`
		CustomPartition apiBool `json:"os_custom_partition"`
		SingleDiskOnly  apiBool `json:"os_single_disk_only"`
		SupportRAID     apiBool `json:"os_support_raid"`
		DedicatedOnly   apiBool `json:"os_dedicated_only"`
		MinRAM          apiInt  `json:"os_minram"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*o = ReinstallOS{
		ID:              r.ID,
		DistributionID:  r.DistributionID,
		Name:            r.Name,
		Release:         r.Release,
		Distribution:    r.Distribution,
		Arch:            r.Arch,
		CustomPartition: bool(r.CustomPartition),
		SingleDiskOnly:  bool(r.SingleDiskOnly),
		SupportRAID:     bool(r.SupportRAID),
		DedicatedOnly:   bool(r.DedicatedOnly),
		MinRAM:          int(r.MinRAM),
	}

	return nil
}

// ListDistributions returns all available distributions for reinstall.
func (s *ReinstallService) ListDistributions(ctx context.Context) ([]Distribution, error) {
	var out []Distribution
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/reinstall/distro",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// ListOperatingSystems returns the OSes available for reinstall of the
// given distribution.
func (s *ReinstallService) ListOperatingSystems(ctx context.Context, distributionID string) ([]ReinstallOS, error) {
	if distributionID == "" {
		return nil, errors.New("gigahost: ListOperatingSystems: distributionID is empty")
	}

	var out []ReinstallOS
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/reinstall/distro/" + url.PathEscape(distributionID),
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// ReinstallRequest is the POST body for /servers/{id}/reinstall.
type ReinstallRequest struct {
	OSID     string `json:"os_id"`
	Language string `json:"language"`
	Keyboard string `json:"keyboard"`
	Timezone string `json:"timezone"`
	Hostname string `json:"hostname"`
}

// ReinstallResult is returned after a successful reinstall initiation.
// The fields sit at the top level of the API response rather than
// inside `data`.
type ReinstallResult struct {
	Message    string `json:"message"`
	Reboot     bool   `json:"reboot"`
	RootPasswd string `json:"root_passwd"`
}

// Reinstall initiates OS reinstallation on a server.
//
// Note: the response arrives at the top of the envelope (next to
// `meta`) rather than inside `data`, so we read the raw bytes and
// decode accordingly.
func (s *ReinstallService) Reinstall(ctx context.Context, serverID string, req ReinstallRequest) (*ReinstallResult, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: Reinstall: serverID is empty")
	}

	var raw []byte
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + url.PathEscape(serverID) + "/reinstall",
		body:   req,
		rawDst: &raw,
	}); err != nil {
		return nil, err
	}

	type flat struct {
		Meta       Meta   `json:"meta"`
		Reboot     bool   `json:"reboot"`
		RootPasswd string `json:"root_passwd"`
	}

	var f flat
	if err := unmarshalJSON(raw, &f); err != nil {
		return nil, fmt.Errorf("gigahost: decode reinstall response: %w", err)
	}

	return &ReinstallResult{
		Message:    f.Meta.Message,
		Reboot:     f.Reboot,
		RootPasswd: f.RootPasswd,
	}, nil
}
