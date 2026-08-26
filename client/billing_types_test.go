package client_test

import (
	"context"
	"net/http"
	"testing"
)

// TestBillingDecodesMixedRepresentations pins upstream B20: one /billing
// response quotes some fields and returns others as bare JSON numbers, in the
// same array. A strict decoder fails the whole payload, so `gigahost account
// balance` and every invoice read break on a real account.
//
// The fixture deliberately carries both forms of every field that has been
// seen unquoted, plus a quoted meta.status — that one would break every
// endpoint in the client, before any service code runs.
func TestBillingDecodesMixedRepresentations(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)
	srv.Expect(http.MethodGet, "/billing").
		RespondFixture(t, "testdata/billing/get_mixed_types.json")

	bill, err := c.Billing.Get(context.Background())
	if err != nil {
		t.Fatalf("Billing.Get: %v", err)
	}

	if len(bill.Invoices) != 2 {
		t.Fatalf("decoded %d invoices, want 2", len(bill.Invoices))
	}

	quoted, unquoted := bill.Invoices[0], bill.Invoices[1]

	for _, tc := range []struct{ name, got, want string }{
		{"quoted total", quoted.Total, "100.00"},
		{"quoted id", quoted.ID, "1"},
		{"unquoted total", unquoted.Total, "100.5"},
		{"unquoted vat", unquoted.VAT, "25"},
		{"unquoted total_vat", unquoted.TotalVAT, "125.5"},
		{"unquoted inv_id", unquoted.ID, "2"},
		{"unquoted order_id", unquoted.OrderID, "20"},
		{"unquoted order_number", unquoted.OrderNumber, "300"},
		{"unquoted inv_number", unquoted.Number, "5"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}

	if len(bill.Credit) != 1 || bill.Credit[0].Amount != "492.2" {
		t.Errorf("credit = %+v, want an amount of 492.2 decoded from a bare number", bill.Credit)
	}
}

// TestAPIStringRejectsNonScalars keeps the leniency narrow. Accepting any
// unquoted token would store the literal text of a bool or an object and
// report success — a shape change upstream would become silent corruption
// rather than a loud decode error.
func TestAPIStringRejectsNonScalars(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{`true`, `{"nested":1}`, `[1,2]`} {
		body := `{"meta":{"status":200},"data":{"invoices":[{"inv_id":` + bad +
			`,"inv_total":"1.00","inv_vat":"0","inv_total_vat":"1.00"}],` +
			`"credit":[],"exchange_rates":[],"orders":[],"card_details":[],"hourly":[]}}`

		srv, c := newServerAndClient(t)
		srv.Expect(http.MethodGet, "/billing").Respond(http.StatusOK, body)

		if _, err := c.Billing.Get(context.Background()); err == nil {
			t.Errorf("decoding inv_id=%s: want an error, got nil", bad)
		}
	}
}
