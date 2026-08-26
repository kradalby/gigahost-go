package client

import (
	"context"
	"time"
)

// InvoicesService handles /my/invoices. Like /my/account, this
// endpoint puts its payload at the top level of the response rather
// than inside `data`.
type InvoicesService struct {
	client *Client
}

// Invoice describes a billing document.
type Invoice struct {
	ID          string
	OrderID     string
	OrderNumber string
	MD5         string
	Filename    string
	Number      string
	Date        time.Time
	DueDate     time.Time
	Paid        bool
	Total       string
	VAT         string
	TotalVAT    string
}

// UnmarshalJSON maps snake_case fields and unix timestamps.
func (i *Invoice) UnmarshalJSON(data []byte) error {
	type raw struct {
		// The id-like fields sit in the same payload as the money fields and
		// arrive unquoted just as readily; one of them breaks the whole
		// decode (upstream B20).
		ID          apiString   `json:"inv_id"`
		OrderID     apiString   `json:"order_id"`
		OrderNumber apiString   `json:"order_number"`
		MD5         string      `json:"inv_md5"`
		Filename    string      `json:"inv_filename"`
		Number      apiString   `json:"inv_number"`
		Date        apiUnixTime `json:"inv_date"`
		DueDate     apiUnixTime `json:"inv_duedate"`
		Paid        apiBool     `json:"inv_paid"`
		// The money fields arrive quoted on some invoices and as bare
		// JSON numbers on others (upstream B20), so they decode via
		// apiString rather than string.
		Total    apiString `json:"inv_total"`
		VAT      apiString `json:"inv_vat"`
		TotalVAT apiString `json:"inv_total_vat"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*i = Invoice{
		ID:          string(r.ID),
		OrderID:     string(r.OrderID),
		OrderNumber: string(r.OrderNumber),
		MD5:         r.MD5,
		Filename:    r.Filename,
		Number:      string(r.Number),
		Date:        time.Time(r.Date),
		DueDate:     time.Time(r.DueDate),
		Paid:        bool(r.Paid),
		Total:       string(r.Total),
		VAT:         string(r.VAT),
		TotalVAT:    string(r.TotalVAT),
	}

	return nil
}

// List returns all invoices for the authenticated account.
func (s *InvoicesService) List(ctx context.Context) ([]Invoice, error) {
	type wrapper struct {
		Success  bool      `json:"success"`
		Invoices []Invoice `json:"invoices"`
	}

	var out wrapper
	if _, err := s.client.do(ctx, requestOptions{
		method:     "GET",
		path:       "/my/invoices",
		dst:        &out,
		noEnvelope: true,
	}); err != nil {
		return nil, err
	}

	return out.Invoices, nil
}
