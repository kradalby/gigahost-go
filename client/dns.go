package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// DNSService groups DNS-related endpoints under /dns.
type DNSService struct {
	client *Client
}

// ZoneType enumerates the supported DNS zone kinds.
type ZoneType string

const (
	ZoneTypeNative ZoneType = "NATIVE"
	ZoneTypeMaster ZoneType = "MASTER"
	ZoneTypeSlave  ZoneType = "SLAVE"
)

// Valid reports whether t is a recognised zone type.
func (t ZoneType) Valid() bool {
	switch t {
	case ZoneTypeNative, ZoneTypeMaster, ZoneTypeSlave:
		return true
	}

	return false
}

// Zone is a DNS zone as returned by /dns/zones.
type Zone struct {
	ID           string
	CustomerID   string
	Name         string
	DisplayName  string
	Type         ZoneType
	Active       bool
	Protected    bool
	IsRegistered bool
	Registrar    string
	DomainStatus string
	ExpiryDate   time.Time
	AutoRenew    bool
	ExternalDNS  bool
	RecordCount  int
	UpdatedAt    time.Time
}

// UnmarshalJSON bridges the API's snake_case fields and string-encoded
// booleans/timestamps into idiomatic Go types.
func (z *Zone) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID           string       `json:"zone_id"`
		CustomerID   string       `json:"cust_id"`
		Name         string       `json:"zone_name"`
		DisplayName  string       `json:"zone_name_display"`
		Type         ZoneType     `json:"zone_type"`
		Active       apiBool      `json:"zone_active"`
		Protected    apiBool      `json:"zone_protected"`
		IsRegistered apiBool      `json:"zone_is_registered"`
		Registrar    string       `json:"domain_registrar"`
		DomainStatus string       `json:"domain_status"`
		ExpiryDate   *apiDateTime `json:"domain_expiry_date"`
		AutoRenew    apiBool      `json:"domain_auto_renew"`
		ExternalDNS  apiBool      `json:"external_dns"`
		RecordCount  int          `json:"record_count"`
		UpdatedAt    apiUnixTime  `json:"zone_updated"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*z = Zone{
		ID:           r.ID,
		CustomerID:   r.CustomerID,
		Name:         r.Name,
		DisplayName:  r.DisplayName,
		Type:         r.Type,
		Active:       bool(r.Active),
		Protected:    bool(r.Protected),
		IsRegistered: bool(r.IsRegistered),
		Registrar:    r.Registrar,
		DomainStatus: r.DomainStatus,
		AutoRenew:    bool(r.AutoRenew),
		ExternalDNS:  bool(r.ExternalDNS),
		RecordCount:  r.RecordCount,
		UpdatedAt:    time.Time(r.UpdatedAt),
	}

	if r.ExpiryDate != nil {
		z.ExpiryDate = time.Time(*r.ExpiryDate)
	}

	return nil
}

// RecordType enumerates common DNS record types. The API accepts any
// RFC-valid record type string; this enumeration is provided for
// convenience and validation only.
type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeAAAA  RecordType = "AAAA"
	RecordTypeCNAME RecordType = "CNAME"
	RecordTypeMX    RecordType = "MX"
	RecordTypeTXT   RecordType = "TXT"
	RecordTypeNS    RecordType = "NS"
	RecordTypeSRV   RecordType = "SRV"
	RecordTypeCAA   RecordType = "CAA"
	RecordTypePTR   RecordType = "PTR"
)

// DNSRecord is a single DNS record within a zone.
type DNSRecord struct {
	ID       string
	Name     string
	Type     RecordType
	Value    string
	TTL      int
	Priority *int
}

// UnmarshalJSON maps snake_case fields. The API returns record_ttl and
// record_priority as numeric strings (and record_id may be a bare number), so
// these are coerced; a null/absent priority decodes to nil.
func (r *DNSRecord) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID       apiString  `json:"record_id"`
		Name     string     `json:"record_name"`
		Type     RecordType `json:"record_type"`
		Value    string     `json:"record_value"`
		TTL      apiInt     `json:"record_ttl"`
		Priority *apiInt    `json:"record_priority"`
	}

	var rr raw
	if err := unmarshalJSON(data, &rr); err != nil {
		return err
	}

	*r = DNSRecord{
		ID:    string(rr.ID),
		Name:  rr.Name,
		Type:  rr.Type,
		Value: rr.Value,
		TTL:   int(rr.TTL),
	}

	if rr.Priority != nil {
		p := int(*rr.Priority)
		r.Priority = &p
	}

	return nil
}

// CreateZoneRequest is the JSON body for POST /dns/zones.
type CreateZoneRequest struct {
	Name                 string   `json:"zone_name"`
	Type                 ZoneType `json:"zone_type,omitzero"`
	CreateDefaultRecords bool     `json:"create_default_records,omitzero"`
	TransferDomain       bool     `json:"transfer_domain,omitzero"`
	AuthCode             string   `json:"auth_code,omitzero"`
	UseExistingNS        bool     `json:"use_existing_ns,omitzero"`
}

// CreateZoneResponse is the decoded `data` of a successful zone create.
type CreateZoneResponse struct {
	ID string `json:"zone_id"`
}

// ListZones returns all DNS zones visible to the authenticated account.
func (s *DNSService) ListZones(ctx context.Context) ([]Zone, error) {
	var zones []Zone
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones",
		dst:    &zones,
	}); err != nil {
		return nil, err
	}

	return zones, nil
}

// CreateZone creates a new zone. POST /dns/zones returns an empty data array
// rather than the new zone, so the ID is resolved by listing zones and matching
// the requested name.
func (s *DNSService) CreateZone(ctx context.Context, req CreateZoneRequest) (*CreateZoneResponse, error) {
	if req.Name == "" {
		return nil, errors.New("gigahost: CreateZone: Name is required")
	}

	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/zones",
		body:   req,
	}); err != nil {
		return nil, err
	}

	zones, err := s.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("gigahost: CreateZone: resolve new zone ID: %w", err)
	}

	for _, z := range zones {
		if z.Name == req.Name {
			return &CreateZoneResponse{ID: z.ID}, nil
		}
	}

	return nil, fmt.Errorf("gigahost: CreateZone: created zone %q not found when listing zones", req.Name)
}

// DeleteZone removes a zone. Protected (registered) zones cannot be
// deleted and will return an APIError with status 400.
func (s *DNSService) DeleteZone(ctx context.Context, zoneID string) error {
	if zoneID == "" {
		return errors.New("gigahost: DeleteZone: zoneID is empty")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/dns/zones/" + zoneID,
	})

	return err
}

// ListRecords returns all records in the given zone.
func (s *DNSService) ListRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: ListRecords: zoneID is empty")
	}

	var out []DNSRecord
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones/" + zoneID + "/records",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// CreateRecordRequest is the JSON body for POST /dns/zones/{id}/records.
type CreateRecordRequest struct {
	Name     string     `json:"record_name,omitzero"`
	Type     RecordType `json:"record_type,omitzero"`
	Value    string     `json:"record_value"`
	TTL      int        `json:"record_ttl,omitzero"`
	Priority *int       `json:"record_priority,omitzero"`
}

// CreateRecord creates a record in the given zone.
func (s *DNSService) CreateRecord(ctx context.Context, zoneID string, req CreateRecordRequest) error {
	if zoneID == "" {
		return errors.New("gigahost: CreateRecord: zoneID is empty")
	}

	if req.Value == "" {
		return errors.New("gigahost: CreateRecord: Value is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/zones/" + zoneID + "/records",
		body:   req,
	})

	return err
}

// UpdateRecordRequest is the JSON body for PUT
// /dns/zones/{id}/records/{record}.
type UpdateRecordRequest struct {
	Name     string     `json:"record_name,omitzero"`
	Type     RecordType `json:"record_type,omitzero"`
	Value    string     `json:"record_value"`
	TTL      int        `json:"record_ttl,omitzero"`
	Priority *int       `json:"record_priority,omitzero"`
}

// UpdateRecord updates a record.
func (s *DNSService) UpdateRecord(ctx context.Context, zoneID, recordID string, req UpdateRecordRequest) error {
	if zoneID == "" || recordID == "" {
		return errors.New("gigahost: UpdateRecord: zoneID and recordID are required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + zoneID + "/records/" + recordID,
		body:   req,
	})

	return err
}

// DeleteRecord removes a record. The API requires the record's name and
// type to be passed as query parameters in addition to the ID in the
// URL path.
func (s *DNSService) DeleteRecord(ctx context.Context, zoneID, recordID, name string, recordType RecordType) error {
	if zoneID == "" || recordID == "" {
		return errors.New("gigahost: DeleteRecord: zoneID and recordID are required")
	}

	q := url.Values{}
	q.Set("name", name)
	q.Set("type", string(recordType))

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/dns/zones/" + zoneID + "/records/" + recordID,
		query:  q,
	})

	return err
}

// Redirect is an HTTP redirect configured for a hosted domain.
type Redirect struct {
	Domain    string
	Source    string
	TargetURL string
	Enabled   bool
	CreatedAt time.Time
}

// UnmarshalJSON handles both the API's "0"/"1" enabled flag and the
// "2024-01-15 12:00:00" timestamp format.
func (r *Redirect) UnmarshalJSON(data []byte) error {
	type raw struct {
		Domain    string       `json:"domain"`
		Source    string       `json:"source"`
		TargetURL string       `json:"target_url"`
		Enabled   apiBool      `json:"enabled"`
		CreatedAt *apiDateTime `json:"created_at"`
	}

	var rr raw
	if err := unmarshalJSON(data, &rr); err != nil {
		return err
	}

	*r = Redirect{
		Domain:    rr.Domain,
		Source:    rr.Source,
		TargetURL: rr.TargetURL,
		Enabled:   bool(rr.Enabled),
	}

	if rr.CreatedAt != nil {
		r.CreatedAt = time.Time(*rr.CreatedAt)
	}

	return nil
}

// ListRedirects returns all redirects configured for the zone.
func (s *DNSService) ListRedirects(ctx context.Context, zoneID string) ([]Redirect, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: ListRedirects: zoneID is empty")
	}

	var out []Redirect
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones/" + zoneID + "/redirect",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// CreateRedirectRequest is the JSON body for POST
// /dns/zones/{id}/redirect.
type CreateRedirectRequest struct {
	// Source is the subdomain to redirect; "@" for the zone apex.
	Source    string `json:"source,omitzero"`
	TargetURL string `json:"target_url"`
}

// CreateRedirect registers a new redirect.
func (s *DNSService) CreateRedirect(ctx context.Context, zoneID string, req CreateRedirectRequest) error {
	if zoneID == "" {
		return errors.New("gigahost: CreateRedirect: zoneID is empty")
	}

	if req.TargetURL == "" {
		return errors.New("gigahost: CreateRedirect: TargetURL is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/zones/" + zoneID + "/redirect",
		body:   req,
	})

	return err
}

// UpdateRedirect updates the target URL of an existing redirect. The
// `source` identifies which redirect to modify.
func (s *DNSService) UpdateRedirect(ctx context.Context, zoneID, source, targetURL string) error {
	if zoneID == "" {
		return errors.New("gigahost: UpdateRedirect: zoneID is empty")
	}

	if targetURL == "" {
		return errors.New("gigahost: UpdateRedirect: targetURL is empty")
	}

	body := map[string]string{"source": source, "target_url": targetURL}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + zoneID + "/redirect",
		body:   body,
	})

	return err
}

// DeleteRedirect removes a redirect for the given source.
func (s *DNSService) DeleteRedirect(ctx context.Context, zoneID, source string) error {
	if zoneID == "" {
		return errors.New("gigahost: DeleteRedirect: zoneID is empty")
	}

	q := url.Values{}
	q.Set("source", source)

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/dns/zones/" + zoneID + "/redirect",
		query:  q,
	})

	return err
}
