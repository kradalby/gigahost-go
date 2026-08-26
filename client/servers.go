package client

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// ServersService groups endpoints under /servers.
type ServersService struct {
	client *Client
}

// ServerOS describes the operating system currently installed on a
// server.
type ServerOS struct {
	ID               string
	Name             string
	Release          string
	DedicatedOnly    bool
	MinRAM           int
	CustomPartition  bool
	SingleDiskOnly   bool
	DistributionLogo string
}

// UnmarshalJSON maps snake_case fields.
func (o *ServerOS) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID               string  `json:"os_id"`
		Name             string  `json:"os_name"`
		Release          string  `json:"os_release"`
		DedicatedOnly    apiBool `json:"os_dedicated_only"`
		MinRAM           apiInt  `json:"os_minram"`
		CustomPartition  apiBool `json:"os_custom_partition"`
		SingleDiskOnly   apiBool `json:"os_single_disk_only"`
		DistributionLogo string  `json:"dist_logo"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*o = ServerOS{
		ID:               r.ID,
		Name:             r.Name,
		Release:          r.Release,
		DedicatedOnly:    bool(r.DedicatedOnly),
		MinRAM:           int(r.MinRAM),
		CustomPartition:  bool(r.CustomPartition),
		SingleDiskOnly:   bool(r.SingleDiskOnly),
		DistributionLogo: r.DistributionLogo,
	}

	return nil
}

// ServerIP is one IP address assigned to a server.
type ServerIP struct {
	ID         string
	SubnetID   string
	Version    string
	Address    string
	Reverse    string
	TrafficSum int64
	PacketsSum int64
	Nullroute  bool
	RoutedTo   string
	Type       string
	Netmask    string
	Gateway    string
}

// UnmarshalJSON maps snake_case fields.
func (ip *ServerIP) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID         string  `json:"ip_id"`
		SubnetID   string  `json:"sub_id"`
		Version    string  `json:"ip_v4v6"`
		Address    string  `json:"ip_address"`
		Reverse    string  `json:"ip_reverse"`
		TrafficSum apiInt  `json:"ip_traffic_sum"`
		PacketsSum apiInt  `json:"ip_pkts_sum"`
		Nullroute  apiBool `json:"ip_nullroute"`
		RoutedTo   string  `json:"ip_routed_to"`
		Type       string  `json:"ip_type"`
		Netmask    string  `json:"ip_netmask"`
		Gateway    string  `json:"ip_gateway"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*ip = ServerIP{
		ID:         r.ID,
		SubnetID:   r.SubnetID,
		Version:    r.Version,
		Address:    r.Address,
		Reverse:    r.Reverse,
		TrafficSum: int64(r.TrafficSum),
		PacketsSum: int64(r.PacketsSum),
		Nullroute:  bool(r.Nullroute),
		RoutedTo:   r.RoutedTo,
		Type:       r.Type,
		Netmask:    r.Netmask,
		Gateway:    r.Gateway,
	}

	return nil
}

// Server is the high-level server record returned by /servers and
// /servers/{id}. The Gigahost API exposes a broad set of fields that
// change meaning depending on whether the server is a VPS or
// dedicated box; the fields here are those consistently documented.
type Server struct {
	ID         string
	Label      string
	Hostname   string
	Name       string
	Type       string
	VPSType    string
	Location   string
	CustomerID string
	ProductID  string
	NodeID     string
	OSID       string
	PrimaryIP  string

	Status         bool
	StatusRescue   bool
	StatusInstall  bool
	StatusSnapshot bool
	StatusMount    bool
	StatusMethod   string
	StatusReboot   int

	Cores     int
	RAM       int
	Bandwidth int

	CreatedAt time.Time

	FeatureReinstall bool
	FeatureMgmt      bool

	Suspended bool

	OS  *ServerOS
	IPs []ServerIP
}

// UnmarshalJSON handles the shape of the Gigahost server record,
// translating numerous "0"/"1" booleans and numeric strings.
func (s *Server) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID         string `json:"srv_id"`
		Label      string `json:"srv_label"`
		Hostname   string `json:"srv_hostname"`
		Name       string `json:"srv_name"`
		Type       string `json:"srv_type"`
		VPSType    string `json:"srv_vps_type"`
		Location   string `json:"srv_location"`
		CustomerID string `json:"cust_id"`
		ProductID  string `json:"product_id"`
		NodeID     string `json:"node_id"`
		OSID       string `json:"os_id"`
		PrimaryIP  string `json:"srv_primary_ip"`

		Status         apiBool `json:"srv_status"`
		StatusRescue   apiBool `json:"srv_status_rescue"`
		StatusInstall  apiBool `json:"srv_status_install"`
		StatusSnapshot apiBool `json:"srv_status_snapshot"`
		StatusMount    apiBool `json:"srv_status_mount"`
		StatusMethod   string  `json:"srv_status_method"`
		StatusReboot   apiInt  `json:"srv_status_reboot"`

		Cores     apiInt `json:"srv_cores"`
		RAM       apiInt `json:"srv_ram"`
		Bandwidth apiInt `json:"srv_bw"`

		CreatedAt apiUnixTime `json:"srv_date_created"`

		FeatureReinstall apiBool `json:"srv_feature_reinstall"`
		FeatureMgmt      apiBool `json:"srv_feature_mgmt"`

		Suspended apiBool `json:"srv_suspended"`

		OS  *ServerOS  `json:"os"`
		IPs []ServerIP `json:"ips"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*s = Server{
		ID:               r.ID,
		Label:            r.Label,
		Hostname:         r.Hostname,
		Name:             r.Name,
		Type:             r.Type,
		VPSType:          r.VPSType,
		Location:         r.Location,
		CustomerID:       r.CustomerID,
		ProductID:        r.ProductID,
		NodeID:           r.NodeID,
		OSID:             r.OSID,
		PrimaryIP:        r.PrimaryIP,
		Status:           bool(r.Status),
		StatusRescue:     bool(r.StatusRescue),
		StatusInstall:    bool(r.StatusInstall),
		StatusSnapshot:   bool(r.StatusSnapshot),
		StatusMount:      bool(r.StatusMount),
		StatusMethod:     r.StatusMethod,
		StatusReboot:     int(r.StatusReboot),
		Cores:            int(r.Cores),
		RAM:              int(r.RAM),
		Bandwidth:        int(r.Bandwidth),
		CreatedAt:        time.Time(r.CreatedAt),
		FeatureReinstall: bool(r.FeatureReinstall),
		FeatureMgmt:      bool(r.FeatureMgmt),
		Suspended:        bool(r.Suspended),
		OS:               r.OS,
		IPs:              r.IPs,
	}

	return nil
}

// List returns all servers visible to the authenticated account.
func (s *ServersService) List(ctx context.Context) ([]Server, error) {
	var out []Server
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// Get returns the full record for a single server. The API wraps the
// single server in a one-element array; this method unwraps it.
func (s *ServersService) Get(ctx context.Context, serverID string) (*Server, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: Servers.Get: serverID is empty")
	}

	var wrapped []Server
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID),
		dst:    &wrapped,
	}); err != nil {
		return nil, err
	}

	if len(wrapped) == 0 {
		return nil, errors.New("gigahost: Servers.Get: empty response")
	}

	return &wrapped[0], nil
}

// UpdateName sets the descriptive name (label) of a server.
func (s *ServersService) UpdateName(ctx context.Context, serverID, name string) error {
	if serverID == "" {
		return errors.New("gigahost: UpdateName: serverID is empty")
	}

	body := map[string]string{"name": name}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/servers/" + url.PathEscape(serverID) + "/name",
		body:   body,
	})

	return err
}

// Cancel terminates a server via POST /servers/{id}/cancel, stopping hourly
// billing. The server is removed from the active server list. This endpoint is
// not in the public API documentation but is the only programmatic teardown for
// servers created via the Deploy service.
func (s *ServersService) Cancel(ctx context.Context, serverID string) error {
	if serverID == "" {
		return errors.New("gigahost: Cancel: serverID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + url.PathEscape(serverID) + "/cancel",
	})

	return err
}

// UpdateReverseRequest is the body for PUT /servers/{id}/reverse.
// Exactly one of IPID (for IPv4) or SubnetID (for IPv6 NS delegation)
// should be set.
type UpdateReverseRequest struct {
	IPID     string `json:"ip_id,omitzero"`
	SubnetID string `json:"sub_id,omitzero"`
	DNS      string `json:"dns"`
}

// UpdateReverse updates the reverse DNS record(s) for a server.
func (s *ServersService) UpdateReverse(ctx context.Context, serverID string, req UpdateReverseRequest) error {
	if serverID == "" {
		return errors.New("gigahost: UpdateReverse: serverID is empty")
	}

	if req.DNS == "" {
		return errors.New("gigahost: UpdateReverse: DNS is required")
	}

	if req.IPID == "" && req.SubnetID == "" {
		return errors.New("gigahost: UpdateReverse: one of IPID or SubnetID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/servers/" + url.PathEscape(serverID) + "/reverse",
		body:   req,
	})

	return err
}

// PortGraphs is the response from /servers/{id}/port_bits and
// /servers/{id}/port_upkts. Fields are base64-encoded PNG images; use
// [Base64Bytes] to decode.
type PortGraphs struct {
	GraphDay   string `json:"graph_day"`
	GraphWeek  string `json:"graph_week"`
	GraphMonth string `json:"graph_month"`
	GraphYear  string `json:"graph_year"`
}

// GetBandwidthGraphs returns base64-encoded bandwidth graphs.
func (s *ServersService) GetBandwidthGraphs(ctx context.Context, serverID string) (*PortGraphs, error) {
	return s.getGraphs(ctx, serverID, "port_bits")
}

// GetPacketGraphs returns base64-encoded packet graphs.
func (s *ServersService) GetPacketGraphs(ctx context.Context, serverID string) (*PortGraphs, error) {
	return s.getGraphs(ctx, serverID, "port_upkts")
}

func (s *ServersService) getGraphs(ctx context.Context, serverID, kind string) (*PortGraphs, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: graph fetch: serverID is empty")
	}

	var out PortGraphs
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + url.PathEscape(serverID) + "/" + url.PathEscape(kind),
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// IPType enumerates the IP order types supported by
// POST /servers/{id}/ipv4.
type IPType string

const (
	IPTypeL2 IPType = "l2"
	IPTypeL3 IPType = "l3"
)

// OrderIPv4 requests a new IPv4 address be assigned to a server.
func (s *ServersService) OrderIPv4(ctx context.Context, serverID string, ipType IPType) error {
	if serverID == "" {
		return errors.New("gigahost: OrderIPv4: serverID is empty")
	}

	body := map[string]string{"ip_type": string(ipType)}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + url.PathEscape(serverID) + "/ipv4",
		body:   body,
	})

	return err
}
