package client

import (
	"context"
	"errors"
	"net/url"
)

// DSRecord represents a DNSSEC delegation signer record.
type DSRecord struct {
	KeyTag     int    `json:"keyTag"`
	Algorithm  int    `json:"alg"`
	DigestType int    `json:"digestType"`
	Digest     string `json:"digest"`
}

// DSRecordsInternal is the response from /dns/zones/{id}/ds-records
// for domains hosted on Gigahost nameservers. The API returns a single
// string per zone.
type DSRecordsInternal struct {
	DSRecords string `json:"ds_records"`
}

// DSRecordsExternal is the response from
// /dns/zones/{id}/ds-records/external. It contains an array of
// structured DS records.
type DSRecordsExternal struct {
	DSRecords []DSRecord `json:"ds_records"`
}

// GetDSRecords retrieves the DS records for a DNSSEC-enabled domain
// hosted on Gigahost nameservers.
func (s *DNSService) GetDSRecords(ctx context.Context, zoneID string) (*DSRecordsInternal, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: GetDSRecords: zoneID is empty")
	}

	var out DSRecordsInternal
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/ds-records",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// GetExternalDSRecords retrieves DS records stored for a domain using
// external nameservers.
func (s *DNSService) GetExternalDSRecords(ctx context.Context, zoneID string) (*DSRecordsExternal, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: GetExternalDSRecords: zoneID is empty")
	}

	var out DSRecordsExternal
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/ds-records/external",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// SubmitExternalDSRecords submits DS records for an externally hosted
// domain to Norid.
func (s *DNSService) SubmitExternalDSRecords(ctx context.Context, zoneID string, records []DSRecord) error {
	if zoneID == "" {
		return errors.New("gigahost: SubmitExternalDSRecords: zoneID is empty")
	}

	if len(records) == 0 {
		return errors.New("gigahost: SubmitExternalDSRecords: at least one DS record is required")
	}

	body := map[string][]DSRecord{"ds_records": records}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/ds-records/external",
		body:   body,
	})

	return err
}

// SetDNSSEC toggles DNSSEC on a registered domain.
func (s *DNSService) SetDNSSEC(ctx context.Context, zoneID string, enabled bool) error {
	if zoneID == "" {
		return errors.New("gigahost: SetDNSSEC: zoneID is empty")
	}

	flag := 0
	if enabled {
		flag = 1
	}

	body := map[string]int{"enable": flag}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/dnssec",
		body:   body,
	})

	return err
}

// IPVersion enumerates IP protocol versions used in PTR zones.
type IPVersion string

const (
	IPVersionIPv4 IPVersion = "ipv4"
	IPVersionIPv6 IPVersion = "ipv6"
)

// CreatePTRZoneRequest is the body for POST /dns/zones/ptr.
type CreatePTRZoneRequest struct {
	Prefix    string    `json:"prefix"`
	IPVersion IPVersion `json:"ip_version"`
	ZoneName  string    `json:"zone_name"`
}

// CreatePTRZoneResponse is the decoded `data` on success.
type CreatePTRZoneResponse struct {
	ZoneID string `json:"zone_id"`
}

// CreatePTRZone creates a new reverse-DNS zone.
func (s *DNSService) CreatePTRZone(ctx context.Context, req CreatePTRZoneRequest) (*CreatePTRZoneResponse, error) {
	if req.Prefix == "" || req.IPVersion == "" || req.ZoneName == "" {
		return nil, errors.New("gigahost: CreatePTRZone: Prefix, IPVersion and ZoneName are required")
	}

	var out CreatePTRZoneResponse
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/zones/ptr",
		body:   req,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
