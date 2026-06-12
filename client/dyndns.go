package client

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// DynDNSService implements the /dns/dyndns endpoint. Unlike every
// other endpoint, DynDNS:
//
//   - uses HTTP Basic authentication rather than a bearer token,
//   - returns a plain-text response rather than JSON, and
//   - accepts multiple hostnames via a comma-separated query parameter
//     and returns one response code per line.
type DynDNSService struct {
	client *Client
}

// DynDNSResponse is a strongly typed representation of the dyndns
// plain-text response codes.
type DynDNSResponse string

const (
	// DynDNSGood indicates the IP was updated successfully.
	DynDNSGood DynDNSResponse = "good"
	// DynDNSNoChange means the stored IP was already correct.
	DynDNSNoChange DynDNSResponse = "nochg"
	// DynDNSNoHost means the hostname is not owned by the account.
	DynDNSNoHost DynDNSResponse = "nohost"
	// DynDNSNotFQDN means the hostname was invalid or missing.
	DynDNSNotFQDN DynDNSResponse = "notfqdn"
	// DynDNSBadAuth means authentication failed.
	DynDNSBadAuth DynDNSResponse = "badauth"
	// DynDNSDNSErr reports a server-side DNS error.
	DynDNSDNSErr DynDNSResponse = "dnserr"
	// DynDNSBadAgent means the IP address was invalid.
	DynDNSBadAgent DynDNSResponse = "badagent"
)

// DynDNSResult is one entry in the multi-hostname response.
type DynDNSResult struct {
	Hostname string
	Response DynDNSResponse
	// IP is populated for "good" and "nochg" responses where the API
	// echoes back the effective IP.
	IP string
}

// UpdateRequest is the parameter set for a dyndns update. At least
// Hostname must be set.
type UpdateRequest struct {
	// Username and Password are the Gigahost account credentials
	// used for HTTP Basic auth. If either is empty the credentials
	// configured on the client via [WithCredentials] are used.
	Username string
	Password string

	// Hostnames is the list of FQDNs to update.
	Hostnames []string

	// IPv4 sets the A record. When empty and IPv6 is also empty the
	// server detects the caller's source IP.
	IPv4 string
	// IPv6 sets the AAAA record.
	IPv6 string
}

// Update calls /dns/dyndns and returns one result per hostname.
func (s *DynDNSService) Update(ctx context.Context, req UpdateRequest) ([]DynDNSResult, error) {
	if len(req.Hostnames) == 0 {
		return nil, errors.New("gigahost: DynDNS.Update: at least one hostname is required")
	}

	user := req.Username
	pass := req.Password

	if user == "" && pass == "" && s.client.credentials != nil {
		user = s.client.credentials.username
		pass = s.client.credentials.password
	}

	if user == "" || pass == "" {
		return nil, errors.New("gigahost: DynDNS.Update: username and password are required for Basic auth")
	}

	q := url.Values{}
	q.Set("hostname", strings.Join(req.Hostnames, ","))

	if req.IPv4 != "" {
		q.Set("myip", req.IPv4)
	}

	if req.IPv6 != "" {
		q.Set("myipv6", req.IPv6)
	}

	var raw []byte

	if _, err := s.client.do(ctx, requestOptions{
		method:   "GET",
		path:     "/dns/dyndns",
		query:    q,
		rawDst:   &raw,
		skipAuth: true,
		basic:    &basicAuth{username: user, password: pass},
	}); err != nil {
		return nil, err
	}

	return parseDynDNSResponse(req.Hostnames, string(raw)), nil
}

func parseDynDNSResponse(hostnames []string, body string) []DynDNSResult {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	out := make([]DynDNSResult, 0, len(lines))

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		result := DynDNSResult{Response: DynDNSResponse(fields[0])}

		if len(fields) > 1 {
			result.IP = fields[1]
		}

		if i < len(hostnames) {
			result.Hostname = hostnames[i]
		}

		out = append(out, result)
	}

	return out
}
