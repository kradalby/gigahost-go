package client_test

import (
	"context"
	"net/http"
	"testing"
)

func TestUpgradesList(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// Shape mirrors the live GET /servers/{id}/upgrade response (product_*
	// fields, no pkg_id — the documented pkg_* shape is stale).
	srv.Expect("GET", "/servers/17533/upgrade").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":[
				{
					"product_id":"7956",
					"product_name":"KVM Value VPS 8GB",
					"product_vm_cores":"4",
					"product_vm_memory":"8",
					"product_vm_storage":"80",
					"product_vm_bw":"20000",
					"product_vm_bw_type":"quota",
					"rate_monthly":"79.00000",
					"currency_code":"NOK"
				}
			]
		}`)

	pkgs, err := c.Upgrades.List(context.Background(), "17533")
	if err != nil {
		t.Fatalf("Upgrades.List: %v", err)
	}

	if len(pkgs) != 1 {
		t.Fatalf("want 1 package, got %d", len(pkgs))
	}

	p := pkgs[0]

	if p.ProductID != "7956" {
		t.Errorf("ProductID = %q, want 7956", p.ProductID)
	}

	if p.ProductName != "KVM Value VPS 8GB" {
		t.Errorf("ProductName = %q", p.ProductName)
	}

	if p.Cores != 4 {
		t.Errorf("Cores = %d, want 4", p.Cores)
	}

	if p.MemoryGB != 8 {
		t.Errorf("MemoryGB = %d, want 8", p.MemoryGB)
	}

	if p.StorageGB != 80 {
		t.Errorf("StorageGB = %d, want 80", p.StorageGB)
	}

	if p.RateMonthly != "79.00000" {
		t.Errorf("RateMonthly = %q", p.RateMonthly)
	}

	if p.Currency != "NOK" {
		t.Errorf("Currency = %q, want NOK", p.Currency)
	}
}

// TestUpgradesApply asserts the request shape we send. The live contract
// is unverified: every probed body form was rejected on the hourly VPS
// (see docs/upstream-issues.md B10), so this guards our side only.
func TestUpgradesApply(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/servers/17533/upgrade").
		WithJSON(`{"pkg_id":"7956"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.Upgrades.Apply(context.Background(), "17533", "7956"); err != nil {
		t.Fatalf("Upgrades.Apply: %v", err)
	}
}

func TestUpgradesValidation(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	if _, err := c.Upgrades.List(context.Background(), ""); err == nil {
		t.Error("expected error for empty serverID")
	}

	if err := c.Upgrades.Apply(context.Background(), "", "7956"); err == nil {
		t.Error("expected error for empty serverID")
	}

	if err := c.Upgrades.Apply(context.Background(), "17533", ""); err == nil {
		t.Error("expected error for empty productID")
	}
}
