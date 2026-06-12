package client

import (
	"context"
	"errors"
)

// UpgradesService handles /servers/{id}/upgrade.
type UpgradesService struct {
	client *Client
}

// UpgradePackage is one product a server can be upgraded (resized) to. The live
// API identifies the target by ProductID; there is no separate package ID
// despite what the documentation shows.
type UpgradePackage struct {
	ProductID     string
	ProductName   string
	Cores         int
	MemoryGB      int
	StorageGB     int
	BandwidthGB   int
	BandwidthType string
	RateMonthly   string
	Currency      string
}

// UnmarshalJSON maps the live product_* fields (the documented pkg_* shape is
// stale).
func (u *UpgradePackage) UnmarshalJSON(data []byte) error {
	type raw struct {
		ProductID     string `json:"product_id"`
		ProductName   string `json:"product_name"`
		Cores         apiInt `json:"product_vm_cores"`
		MemoryGB      apiInt `json:"product_vm_memory"`
		StorageGB     apiInt `json:"product_vm_storage"`
		BandwidthGB   apiInt `json:"product_vm_bw"`
		BandwidthType string `json:"product_vm_bw_type"`
		RateMonthly   string `json:"rate_monthly"`
		Currency      string `json:"currency_code"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*u = UpgradePackage{
		ProductID:     r.ProductID,
		ProductName:   r.ProductName,
		Cores:         int(r.Cores),
		MemoryGB:      int(r.MemoryGB),
		StorageGB:     int(r.StorageGB),
		BandwidthGB:   int(r.BandwidthGB),
		BandwidthType: r.BandwidthType,
		RateMonthly:   r.RateMonthly,
		Currency:      r.Currency,
	}

	return nil
}

// List returns the available upgrade packages for a server.
func (s *UpgradesService) List(ctx context.Context, serverID string) ([]UpgradePackage, error) {
	if serverID == "" {
		return nil, errors.New("gigahost: Upgrades.List: serverID is empty")
	}

	var out []UpgradePackage
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/servers/" + serverID + "/upgrade",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// Apply upgrades (resizes) a server to the target product. productID is an
// UpgradePackage.ProductID from List.
//
// NOTE: the exact request contract is unverified. The documentation says the
// body field is "pkg_id", but the live upgrade list no longer returns a pkg_id,
// and POSTs with pkg_id/product_id/upgrade_id were rejected on a server that was
// still installing — the endpoint likely requires a fully provisioned server.
// This needs a live probe against a ready server before it can be relied on.
func (s *UpgradesService) Apply(ctx context.Context, serverID, productID string) error {
	if serverID == "" || productID == "" {
		return errors.New("gigahost: Upgrades.Apply: serverID and productID are required")
	}

	body := map[string]string{"pkg_id": productID}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/servers/" + serverID + "/upgrade",
		body:   body,
	})

	return err
}
