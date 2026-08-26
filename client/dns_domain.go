package client

import (
	"context"
	"errors"
	"net/url"
)

// OrganizationLookup is the result of looking up a Norwegian
// organisation in Brønnøysundregistrene.
type OrganizationLookup struct {
	Name    string `json:"company_name"`
	Address string `json:"address"`
	Zip     string `json:"zip_code"`
	City    string `json:"city"`
}

// LookupOrganization resolves a nine-digit Norwegian organisation
// number.
func (s *DNSService) LookupOrganization(ctx context.Context, orgNumber string) (*OrganizationLookup, error) {
	if orgNumber == "" {
		return nil, errors.New("gigahost: LookupOrganization: orgNumber is empty")
	}

	var out OrganizationLookup
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/lookup/organization/" + url.PathEscape(orgNumber),
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// DomainCheck is the result of /dns/domains/check/{domain}.
type DomainCheck struct {
	Domain    string `json:"domain"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// CheckDomain returns whether a .no domain is available for
// registration.
func (s *DNSService) CheckDomain(ctx context.Context, domain string) (*DomainCheck, error) {
	if domain == "" {
		return nil, errors.New("gigahost: CheckDomain: domain is empty")
	}

	var out DomainCheck
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/domains/check/" + url.PathEscape(domain),
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// RegistrantType is the registrant kind (organisation or private
// person) used in Norid registrations.
type RegistrantType string

const (
	RegistrantTypeOrganization RegistrantType = "organization"
	RegistrantTypePerson       RegistrantType = "person"
)

// RegisterDomainRequest is the POST body for /dns/domains/register.
type RegisterDomainRequest struct {
	DomainName     string         `json:"domain_name"`
	RegistrantType RegistrantType `json:"registrant_type"`
	Email          string         `json:"email"`
	ApplicantName  string         `json:"applicant_name"`
	ZipCode        string         `json:"zip_code"`
	City           string         `json:"city"`
	OrgNumber      string         `json:"org_number,omitzero"`
	CompanyName    string         `json:"company_name,omitzero"`
	PID            string         `json:"pid,omitzero"`
	FirstName      string         `json:"first_name,omitzero"`
	LastName       string         `json:"last_name,omitzero"`
	UseGigahostNS  *bool          `json:"use_gigahost_ns,omitzero"`
	Nameservers    []string       `json:"nameservers,omitzero"`
}

// RegisterDomainResponse is the decoded `data` of a successful
// registration.
type RegisterDomainResponse struct {
	ZoneID     string `json:"zone_id"`
	DomainName string `json:"domain_name"`
	ExpiresAt  string `json:"expires_at"`
	Status     string `json:"status"`
}

// RegisterDomain registers a new .no domain via Norid.
func (s *DNSService) RegisterDomain(ctx context.Context, req RegisterDomainRequest) (*RegisterDomainResponse, error) {
	if req.DomainName == "" || req.RegistrantType == "" || req.Email == "" {
		return nil, errors.New("gigahost: RegisterDomain: DomainName, RegistrantType and Email are required")
	}

	var out RegisterDomainResponse
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/dns/domains/register",
		body:   req,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// Registrant is the contact information stored at Norid for a
// registered .no domain.
type Registrant struct {
	ContactID    string `json:"contact_id"`
	Name         string `json:"name"`
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Address      string `json:"address"`
	City         string `json:"city"`
	PostalCode   string `json:"postal_code"`
	CountryCode  string `json:"country_code"`
	Identity     string `json:"identity,omitzero"`
	IdentityType string `json:"identity_type,omitzero"`
	Type         string `json:"type"`
}

// GetRegistrant retrieves the registrant information for a registered
// .no domain.
func (s *DNSService) GetRegistrant(ctx context.Context, zoneID string) (*Registrant, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: GetRegistrant: zoneID is empty")
	}

	var out Registrant
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/registrant",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// UpdateRegistrantRequest is the PUT body for
// /dns/zones/{id}/registrant. Fields are identical to
// [RegisterDomainRequest] minus the domain name.
type UpdateRegistrantRequest struct {
	RegistrantType RegistrantType `json:"registrant_type"`
	Email          string         `json:"email"`
	ApplicantName  string         `json:"applicant_name"`
	ZipCode        string         `json:"zip_code"`
	City           string         `json:"city"`
	AgreeToTerms   bool           `json:"agree_to_terms"`
	OrgNumber      string         `json:"org_number,omitzero"`
	CompanyName    string         `json:"company_name,omitzero"`
	PID            string         `json:"pid,omitzero"`
}

// UpdateRegistrant changes the registrant of a registered .no domain.
func (s *DNSService) UpdateRegistrant(ctx context.Context, zoneID string, req UpdateRegistrantRequest) error {
	if zoneID == "" {
		return errors.New("gigahost: UpdateRegistrant: zoneID is empty")
	}

	if !req.AgreeToTerms {
		return errors.New("gigahost: UpdateRegistrant: AgreeToTerms must be true (Norid Applicant Declaration)")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/registrant",
		body:   req,
	})

	return err
}

// SetAutoRenew toggles automatic renewal for a registered domain.
func (s *DNSService) SetAutoRenew(ctx context.Context, zoneID string, enabled bool) error {
	if zoneID == "" {
		return errors.New("gigahost: SetAutoRenew: zoneID is empty")
	}

	flag := 0
	if enabled {
		flag = 1
	}

	body := map[string]int{"auto_renew": flag}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/auto-renew",
		body:   body,
	})

	return err
}

// SetNameservers updates the nameservers used by a registered domain.
// A minimum of two nameservers is required by Norid.
func (s *DNSService) SetNameservers(ctx context.Context, zoneID string, nameservers []string) error {
	if zoneID == "" {
		return errors.New("gigahost: SetNameservers: zoneID is empty")
	}

	if len(nameservers) < 2 {
		return errors.New("gigahost: SetNameservers: at least two nameservers are required")
	}

	body := map[string][]string{"nameservers": nameservers}

	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/nameservers",
		body:   body,
	})

	return err
}

// SetRegistrantEmailRequest is the body for PUT
// /dns/zones/{id}/registrant-email.
type SetRegistrantEmailRequest struct {
	Email            string `json:"email"`
	EnableProtection bool   `json:"enable_protection"`
}

// SetRegistrantEmailResponse is the decoded `data` on success.
type SetRegistrantEmailResponse struct {
	Protected bool   `json:"protected"`
	Email     string `json:"email"`
}

// SetRegistrantEmail updates the registrant email, optionally enabling
// WHOIS email protection (forwarding alias).
func (s *DNSService) SetRegistrantEmail(ctx context.Context, zoneID string, req SetRegistrantEmailRequest) (*SetRegistrantEmailResponse, error) {
	if zoneID == "" {
		return nil, errors.New("gigahost: SetRegistrantEmail: zoneID is empty")
	}

	if req.Email == "" {
		return nil, errors.New("gigahost: SetRegistrantEmail: Email is required")
	}

	var out SetRegistrantEmailResponse
	if _, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/dns/zones/" + url.PathEscape(zoneID) + "/registrant-email",
		body:   req,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
