package client

import (
	"context"
	"errors"
	"net/url"
)

// IPMIService handles /servers/{id}/ipmi.
type IPMIService struct {
	client *Client
}

// IPMISession is a short-lived KVM/IPMI session. Sessions expire after
// roughly three hours.
type IPMISession struct {
	IPAddress string `json:"kvm_ip_address"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// Create requests a new IPMI session. `acl` is a semicolon-separated
// list of IPs and/or CIDR subnets permitted to connect.
func (s *IPMIService) Create(ctx context.Context, serverID, acl string) (*IPMISession, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: IPMI.Create: serverID is empty")
	}

	body := map[string]string{"acl": acl}

	var out IPMISession
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + url.PathEscape(serverID) + "/ipmi",
		body:   body,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
