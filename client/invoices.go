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
		ID          string      `json:"inv_id"`
		OrderID     string      `json:"order_id"`
		OrderNumber string      `json:"order_number"`
		MD5         string      `json:"inv_md5"`
		Filename    string      `json:"inv_filename"`
		Number      string      `json:"inv_number"`
		Date        apiUnixTime `json:"inv_date"`
		DueDate     apiUnixTime `json:"inv_duedate"`
		Paid        apiBool     `json:"inv_paid"`
		Total       string      `json:"inv_total"`
		VAT         string      `json:"inv_vat"`
		TotalVAT    string      `json:"inv_total_vat"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*i = Invoice{
		ID:          r.ID,
		OrderID:     r.OrderID,
		OrderNumber: r.OrderNumber,
		MD5:         r.MD5,
		Filename:    r.Filename,
		Number:      r.Number,
		Date:        time.Time(r.Date),
		DueDate:     time.Time(r.DueDate),
		Paid:        bool(r.Paid),
		Total:       r.Total,
		VAT:         r.VAT,
		TotalVAT:    r.TotalVAT,
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
