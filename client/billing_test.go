package client_test

import (
	"context"
	"testing"
)

func TestBillingGet(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/billing").
		RespondFixture(t, "testdata/billing/get.json")

	bill, err := c.Billing.Get(context.Background())
	if err != nil {
		t.Fatalf("Billing.Get: %v", err)
	}

	if len(bill.Credit) != 1 {
		t.Fatalf("want 1 credit, got %d", len(bill.Credit))
	}

	if bill.Credit[0].Currency != "NOK" {
		t.Errorf("Currency = %q, want NOK", bill.Credit[0].Currency)
	}

	if bill.Credit[0].Amount != "500.00" {
		t.Errorf("Amount = %q, want 500.00", bill.Credit[0].Amount)
	}
}
