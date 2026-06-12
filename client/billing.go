package client

import "context"

// BillingService handles the /billing endpoint, which exposes the account's
// prepaid credit, invoices and running hourly servers. It is the accessible
// alternative to the /my/* endpoints, which require a permission the typical
// API key lacks.
type BillingService struct {
	client *Client
}

// BillingCredit is one prepaid-credit balance, per currency.
type BillingCredit struct {
	Currency string
	Amount   string // decimal string, e.g. "500.00"
}

// UnmarshalJSON maps snake_case API fields.
func (c *BillingCredit) UnmarshalJSON(data []byte) error {
	type raw struct {
		Currency string `json:"credit_currency"`
		Amount   string `json:"credit_amount"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*c = BillingCredit(r)

	return nil
}

// Billing is the response from GET /billing.
type Billing struct {
	Credit   []BillingCredit
	Invoices []Invoice
}

// UnmarshalJSON maps the billing envelope, ignoring fields we do not model.
func (b *Billing) UnmarshalJSON(data []byte) error {
	type raw struct {
		Credit   []BillingCredit `json:"credit"`
		Invoices []Invoice       `json:"invoices"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*b = Billing(r)

	return nil
}

// Get returns the billing overview: prepaid credit and invoices.
func (s *BillingService) Get(ctx context.Context) (*Billing, error) {
	var out Billing
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/billing",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
