package client

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// BGPService groups BGP-related endpoints under /bgp.
type BGPService struct {
	client *Client
}

// BGPASN represents a customer's AS number record.
type BGPASN struct {
	ID             string
	ASN            string
	Name           string
	Country        string
	IRRv4          string
	IRRv6          string
	IRRUpdated     apiUnixTime
	Status         string
	RejectedReason string
}

// UnmarshalJSON bridges snake_case API fields to idiomatic Go names.
func (a *BGPASN) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID             string      `json:"id"`
		ASN            string      `json:"asn"`
		Name           string      `json:"asn_name"`
		Country        string      `json:"asn_country"`
		IRRv4          string      `json:"irr_v4"`
		IRRv6          string      `json:"irr_v6"`
		IRRUpdated     apiUnixTime `json:"irr_updated"`
		Status         string      `json:"status"`
		RejectedReason string      `json:"rejected_reason"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*a = BGPASN(r)

	return nil
}

// BGPPrefixList is an entry in a BGP prefix list.
type BGPPrefixList struct {
	ID         string
	ASNID      string
	Prefix     string
	PrefixType string
	Status     string
	YourASN    string
	Country    string
}

// UnmarshalJSON maps snake_case fields.
func (p *BGPPrefixList) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID         string `json:"id"`
		ASNID      string `json:"asn_id"`
		Prefix     string `json:"prefix"`
		PrefixType string `json:"prefix_type"`
		Status     string `json:"status"`
		YourASN    string `json:"your_asn"`
		Country    string `json:"asn_country"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*p = BGPPrefixList(r)

	return nil
}

// BGPSession is one BGP peering session.
type BGPSession struct {
	ID           string
	ASNID        string
	CustomerID   string
	RouterID     string
	ServerID     string
	IPID         string
	IPType       string
	DefaultRoute bool
	Status       string
	NeighborIPv4 string
	NeighborIPv6 string
	Multihop     bool
	RouterASN    string
	YourASN      string
	Country      string
	IPAddress    string
}

// UnmarshalJSON maps snake_case and "0"/"1" booleans.
func (s *BGPSession) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID           string  `json:"id"`
		ASNID        string  `json:"asn_id"`
		CustomerID   string  `json:"cust_id"`
		RouterID     string  `json:"router_id"`
		ServerID     string  `json:"srv_id"`
		IPID         string  `json:"ip_id"`
		IPType       string  `json:"ip_type"`
		DefaultRoute apiBool `json:"defaultroute"`
		Status       string  `json:"status"`
		NeighborIPv4 string  `json:"neighbor_ipv4"`
		NeighborIPv6 string  `json:"neighbor_ipv6"`
		Multihop     apiBool `json:"multihop"`
		RouterASN    string  `json:"router_asn"`
		YourASN      string  `json:"your_asn"`
		Country      string  `json:"asn_country"`
		IPAddress    string  `json:"ip_address"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*s = BGPSession{
		ID:           r.ID,
		ASNID:        r.ASNID,
		CustomerID:   r.CustomerID,
		RouterID:     r.RouterID,
		ServerID:     r.ServerID,
		IPID:         r.IPID,
		IPType:       r.IPType,
		DefaultRoute: bool(r.DefaultRoute),
		Status:       r.Status,
		NeighborIPv4: r.NeighborIPv4,
		NeighborIPv6: r.NeighborIPv6,
		Multihop:     bool(r.Multihop),
		RouterASN:    r.RouterASN,
		YourASN:      r.YourASN,
		Country:      r.Country,
		IPAddress:    r.IPAddress,
	}

	return nil
}

// BGPData is the aggregate response for GET /bgp.
type BGPData struct {
	ASNs        []BGPASN        `json:"asn"`
	PrefixLists []BGPPrefixList `json:"prefix_lists"`
	Sessions    []BGPSession    `json:"sessions"`
}

// Get fetches all BGP data associated with the authenticated account.
func (s *BGPService) Get(ctx context.Context) (*BGPData, error) {
	var out BGPData
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/bgp",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// SubmitASN submits a new ASN for BGP peering approval. The API accepts
// either the numeric form "212345" or the prefixed form "AS212345";
// this method normalises the latter to the former before sending.
func (s *BGPService) SubmitASN(ctx context.Context, asn string) error {
	asn = strings.TrimSpace(asn)
	if asn == "" {
		return errors.New("gigahost: SubmitASN: asn is empty")
	}

	if strings.HasPrefix(strings.ToUpper(asn), "AS") {
		asn = asn[2:]
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/bgp/asn",
		body:   map[string]string{"asn": asn},
	})

	return err
}

// CreateBGPSessionRequest is the POST body for /bgp/{asn_id}/session.
type CreateBGPSessionRequest struct {
	Redundant    bool   `json:"-"`
	DefaultRoute bool   `json:"-"`
	IPIDv4       string `json:"ip_id_v4,omitempty"`
	IPIDv6       string `json:"ip_id_v6,omitempty"`
}

// MarshalJSON maps booleans to "0"/"1" integers the API expects.
func (r CreateBGPSessionRequest) MarshalJSON() ([]byte, error) {
	type out struct {
		Redundant    int    `json:"redundant"`
		DefaultRoute int    `json:"defaultroute"`
		IPIDv4       string `json:"ip_id_v4,omitempty"`
		IPIDv6       string `json:"ip_id_v6,omitempty"`
	}

	o := out{IPIDv4: r.IPIDv4, IPIDv6: r.IPIDv6}

	if r.Redundant {
		o.Redundant = 1
	}

	if r.DefaultRoute {
		o.DefaultRoute = 1
	}

	return marshalJSON(o)
}

// CreateSession creates BGP peering session(s) for an approved ASN.
// At least one of IPIDv4 or IPIDv6 must be set.
func (s *BGPService) CreateSession(ctx context.Context, asnID string, req CreateBGPSessionRequest) error {
	if asnID == "" {
		return errors.New("gigahost: CreateSession: asnID is empty")
	}

	if req.IPIDv4 == "" && req.IPIDv6 == "" {
		return errors.New("gigahost: CreateSession: at least one of IPIDv4 or IPIDv6 is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/bgp/" + url.PathEscape(asnID) + "/session",
		body:   req,
	})

	return err
}

// DeleteSession marks a BGP session for deletion. Sessions are removed
// asynchronously server-side.
func (s *BGPService) DeleteSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.New("gigahost: DeleteSession: sessionID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/bgp/" + url.PathEscape(sessionID) + "/session",
	})

	return err
}
