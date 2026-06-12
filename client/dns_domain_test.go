package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

// Registrant/registration payloads are synthesized from the API docs;
// the test account holds no registered .no domain, so no captured live
// fixtures exist for these endpoints.

func TestDNSLookupOrganization(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/lookup/organization/915933149").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"company_name":"EXAMPLE AS","address":"Storgata 1","zip_code":"0155","city":"OSLO"}}`)

	org, err := c.DNS.LookupOrganization(context.Background(), "915933149")
	if err != nil {
		t.Fatalf("LookupOrganization: %v", err)
	}

	if org.Name != "EXAMPLE AS" || org.City != "OSLO" {
		t.Errorf("OrganizationLookup = %+v", org)
	}

	if _, err := c.DNS.LookupOrganization(context.Background(), ""); err == nil {
		t.Error("expected error for empty orgNumber")
	}
}

func TestDNSRegisterDomain(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/dns/domains/register").
		WithJSON(`{"domain_name":"example.no","registrant_type":"organization","email":"post@example.no","applicant_name":"Ola Nordmann","zip_code":"0155","city":"OSLO","org_number":"915933149","company_name":"EXAMPLE AS"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"zone_id":"7777","domain_name":"example.no","expires_at":"2027-06-11","status":"registered"}}`)

	res, err := c.DNS.RegisterDomain(context.Background(), client.RegisterDomainRequest{
		DomainName:     "example.no",
		RegistrantType: client.RegistrantTypeOrganization,
		Email:          "post@example.no",
		ApplicantName:  "Ola Nordmann",
		ZipCode:        "0155",
		City:           "OSLO",
		OrgNumber:      "915933149",
		CompanyName:    "EXAMPLE AS",
	})
	if err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}

	if res.ZoneID != "7777" || res.Status != "registered" {
		t.Errorf("RegisterDomainResponse = %+v", res)
	}

	if _, err := c.DNS.RegisterDomain(context.Background(), client.RegisterDomainRequest{}); err == nil {
		t.Error("expected error for missing required fields")
	}
}

func TestDNSGetRegistrant(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/zones/7777/registrant").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"contact_id":"N123","name":"Ola Nordmann","organization":"EXAMPLE AS","email":"post@example.no","address":"Storgata 1","city":"OSLO","postal_code":"0155","country_code":"NO","type":"organization"}}`)

	reg, err := c.DNS.GetRegistrant(context.Background(), "7777")
	if err != nil {
		t.Fatalf("GetRegistrant: %v", err)
	}

	if reg.ContactID != "N123" || reg.Organization != "EXAMPLE AS" {
		t.Errorf("Registrant = %+v", reg)
	}

	if _, err := c.DNS.GetRegistrant(context.Background(), ""); err == nil {
		t.Error("expected error for empty zoneID")
	}
}

func TestDNSUpdateRegistrant(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/dns/zones/7777/registrant").
		WithJSON(`{"registrant_type":"person","email":"ola@example.no","applicant_name":"Ola Nordmann","zip_code":"0155","city":"OSLO","agree_to_terms":true,"pid":"12345"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.DNS.UpdateRegistrant(context.Background(), "7777", client.UpdateRegistrantRequest{
		RegistrantType: client.RegistrantTypePerson,
		Email:          "ola@example.no",
		ApplicantName:  "Ola Nordmann",
		ZipCode:        "0155",
		City:           "OSLO",
		AgreeToTerms:   true,
		PID:            "12345",
	})
	if err != nil {
		t.Fatalf("UpdateRegistrant: %v", err)
	}

	if err := c.DNS.UpdateRegistrant(context.Background(), "", client.UpdateRegistrantRequest{AgreeToTerms: true}); err == nil {
		t.Error("expected error for empty zoneID")
	}

	if err := c.DNS.UpdateRegistrant(context.Background(), "7777", client.UpdateRegistrantRequest{}); err == nil {
		t.Error("expected error when AgreeToTerms is false")
	}
}

func TestDNSSetAutoRenew(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/dns/zones/7777/auto-renew").
		WithJSON(`{"auto_renew":1}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.DNS.SetAutoRenew(context.Background(), "7777", true); err != nil {
		t.Fatalf("SetAutoRenew: %v", err)
	}

	srv.Expect("PUT", "/dns/zones/7777/auto-renew").
		WithJSON(`{"auto_renew":0}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.DNS.SetAutoRenew(context.Background(), "7777", false); err != nil {
		t.Fatalf("SetAutoRenew(false): %v", err)
	}

	if err := c.DNS.SetAutoRenew(context.Background(), "", true); err == nil {
		t.Error("expected error for empty zoneID")
	}
}

func TestDNSSetNameservers(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/dns/zones/7777/nameservers").
		WithJSON(`{"nameservers":["ns1.example.no","ns2.example.no"]}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.DNS.SetNameservers(context.Background(), "7777", []string{"ns1.example.no", "ns2.example.no"})
	if err != nil {
		t.Fatalf("SetNameservers: %v", err)
	}

	if err := c.DNS.SetNameservers(context.Background(), "", []string{"a", "b"}); err == nil {
		t.Error("expected error for empty zoneID")
	}

	if err := c.DNS.SetNameservers(context.Background(), "7777", []string{"only-one"}); err == nil {
		t.Error("expected error for fewer than two nameservers")
	}
}

func TestDNSSetRegistrantEmail(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/dns/zones/7777/registrant-email").
		WithJSON(`{"email":"post@example.no","enable_protection":true}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"protected":true,"email":"post@example.no"}}`)

	res, err := c.DNS.SetRegistrantEmail(context.Background(), "7777", client.SetRegistrantEmailRequest{
		Email:            "post@example.no",
		EnableProtection: true,
	})
	if err != nil {
		t.Fatalf("SetRegistrantEmail: %v", err)
	}

	if !res.Protected || res.Email != "post@example.no" {
		t.Errorf("SetRegistrantEmailResponse = %+v", res)
	}

	if _, err := c.DNS.SetRegistrantEmail(context.Background(), "", client.SetRegistrantEmailRequest{Email: "x"}); err == nil {
		t.Error("expected error for empty zoneID")
	}

	if _, err := c.DNS.SetRegistrantEmail(context.Background(), "7777", client.SetRegistrantEmailRequest{}); err == nil {
		t.Error("expected error for empty email")
	}
}
