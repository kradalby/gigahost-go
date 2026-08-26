//go:build e2e

package e2e

import (
	"testing"
)

// TestAuth confirms the token is valid and the account decodes.
func TestAuth(t *testing.T) {
	c := newClient(t)

	acc, err := c.Account.Get(testContext(t))
	if err != nil {
		t.Fatalf("Account.Get: %v", err)
	}

	if acc.CustomerID == "" {
		t.Fatal("account customer ID is empty")
	}

	t.Logf("authenticated as customer %s (partner=%v)", acc.CustomerID, acc.IsPartner)
}

// TestDeployCatalog verifies the live catalog decodes and that a cheapest
// deploy target resolves with a price ID (guards the price_id work).
func TestDeployCatalog(t *testing.T) {
	c := newClient(t)

	cat, err := c.Deploy.GetCatalog(testContext(t))
	if err != nil {
		t.Fatalf("Deploy.GetCatalog: %v", err)
	}

	if len(cat.Tiers) == 0 {
		t.Fatal("catalog has no tiers")
	}

	target := cheapestTarget(t, c)
	if target.PriceID == "" || target.ProductID == "" || target.RegionID == "" {
		t.Fatalf("cheapest target incomplete: %+v", target)
	}

	t.Logf("cheapest: %s (product %s, price %s, region %s, %.5f %s/hr)",
		target.Product.Name, target.ProductID, target.PriceID, target.RegionID,
		target.Product.RateHourly, cat.Currency)
}

// TestBilling verifies the /billing credit decodes.
func TestBilling(t *testing.T) {
	c := newClient(t)

	bill, err := c.Billing.Get(testContext(t))
	if err != nil {
		t.Fatalf("Billing.Get: %v", err)
	}

	if len(bill.Credit) == 0 {
		t.Fatal("no credit entries returned")
	}

	for _, cr := range bill.Credit {
		if cr.Currency == "" || cr.Amount == "" {
			t.Errorf("incomplete credit entry: %+v", cr)
		}

		t.Logf("credit: %s %s", cr.Amount, cr.Currency)
	}
}
