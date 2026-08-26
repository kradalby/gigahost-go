package client

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// AccountService handles /account and /my/account endpoints.
type AccountService struct {
	client *Client
}

// Contact is one user contact returned inside the account profile.
// The shape maps the /account endpoint's contacts array.
type Contact struct {
	ID          string
	Name        string
	Username    string // email address used to log in
	AccessLevel string // "admin", "user", or "server"
	TwoFA       bool
}

// UnmarshalJSON maps snake_case fields from the /account contacts array.
func (c *Contact) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          string  `json:"contact_id"`
		Name        string  `json:"contact_name"`
		Username    string  `json:"contact_username"`
		AccessLevel string  `json:"contact_access_level"`
		TwoFA       apiBool `json:"contact_2fa"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*c = Contact{
		ID:          r.ID,
		Name:        r.Name,
		Username:    r.Username,
		AccessLevel: r.AccessLevel,
		TwoFA:       bool(r.TwoFA),
	}

	return nil
}

// SSHKey is one SSH public key stored on the account.
type SSHKey struct {
	ID      string
	Name    string
	AddedAt time.Time
	Data    string
}

// UnmarshalJSON maps snake_case API fields.
func (k *SSHKey) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID      string      `json:"key_id"`
		Name    string      `json:"key_name"`
		AddedAt apiUnixTime `json:"key_added"`
		Data    string      `json:"key_data"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*k = SSHKey{
		ID:      r.ID,
		Name:    r.Name,
		AddedAt: time.Time(r.AddedAt),
		Data:    r.Data,
	}

	return nil
}

// Passkey is one registered passkey (WebAuthn) on the account.
type Passkey struct {
	ID        string
	Name      string
	CreatedAt time.Time
	LastUsed  time.Time
}

// UnmarshalJSON maps snake_case API fields.
func (p *Passkey) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID        string      `json:"passkey_id"`
		Name      string      `json:"passkey_name"`
		CreatedAt apiUnixTime `json:"created_at"`
		LastUsed  apiUnixTime `json:"last_used"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*p = Passkey{
		ID:        r.ID,
		Name:      r.Name,
		CreatedAt: time.Time(r.CreatedAt),
		LastUsed:  time.Time(r.LastUsed),
	}

	return nil
}

// AccountOrderProduct is one product line item inside an order.
type AccountOrderProduct struct {
	ID          string
	ServerID    string
	ProductID   string
	ProductName string
	ServerName  string
}

// UnmarshalJSON maps snake_case API fields.
func (p *AccountOrderProduct) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID          string `json:"op_id"`
		ServerID    string `json:"srv_id"`
		ProductID   string `json:"product_id"`
		ProductName string `json:"product_name"`
		ServerName  string `json:"srv_name"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*p = AccountOrderProduct(r)

	return nil
}

// AccountOrder is one order associated with the account.
type AccountOrder struct {
	ID           string
	Number       string
	Status       string
	BillingCycle string
	Products     []AccountOrderProduct
}

// UnmarshalJSON maps snake_case API fields.
func (o *AccountOrder) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID           string                `json:"order_id"`
		Number       string                `json:"order_number"`
		Status       string                `json:"order_status"`
		BillingCycle string                `json:"order_billing_cycle"`
		Products     []AccountOrderProduct `json:"products"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*o = AccountOrder(r)

	return nil
}

// ActivityLogEntry is one entry in the account activity log.
type ActivityLogEntry struct {
	ID              string
	ContactID       string
	ContactName     string
	ContactUsername string
	Timestamp       time.Time
	Entry           string
}

// UnmarshalJSON maps snake_case API fields.
func (e *ActivityLogEntry) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID              string      `json:"log_id"`
		ContactID       string      `json:"contact_id"`
		ContactName     string      `json:"contact_name"`
		ContactUsername string      `json:"contact_username"`
		Timestamp       apiUnixTime `json:"log_timestamp"`
		Entry           string      `json:"log_entry"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*e = ActivityLogEntry{
		ID:              r.ID,
		ContactID:       r.ContactID,
		ContactName:     r.ContactName,
		ContactUsername: r.ContactUsername,
		Timestamp:       time.Time(r.Timestamp),
		Entry:           r.Entry,
	}

	return nil
}

// Account is the authenticated customer's account profile, returned by
// GET /account.
type Account struct {
	CustomerID   string
	Name         string
	CompanyNo    string
	Address      string
	Address2     string
	Province     string
	ZipCode      string
	City         string
	Country      string
	Phone        string
	Email        string
	BillingEmail string
	IsPartner    bool

	// Notification preferences.
	Newsletter            bool
	Incident              bool
	BandwidthNotification bool
	EmailOnLogin          bool
	NotifyServiceRenewal  bool

	SSHKeys  []SSHKey
	Passkeys []Passkey
	Contacts []Contact
	Orders   []AccountOrder
}

// UnmarshalJSON handles the standard-envelope /account response shape.
func (a *Account) UnmarshalJSON(data []byte) error {
	type raw struct {
		CustomerID   string  `json:"cust_id"`
		Name         string  `json:"cust_name"`
		CompanyNo    string  `json:"cust_company_no"`
		Address      string  `json:"cust_address"`
		Address2     string  `json:"cust_address2"`
		Province     string  `json:"cust_province"`
		ZipCode      string  `json:"cust_zipcode"`
		City         string  `json:"cust_city"`
		Country      string  `json:"cust_country"`
		Phone        string  `json:"cust_phone"`
		Email        string  `json:"cust_email"`
		BillingEmail string  `json:"cust_billing_email"`
		IsPartner    apiBool `json:"cust_partner"`

		Newsletter            bool `json:"cust_newsletter"`
		Incident              bool `json:"cust_incident"`
		BandwidthNotification bool `json:"cust_bandwidth_notification"`
		EmailOnLogin          bool `json:"cust_email_on_login"`
		NotifyServiceRenewal  bool `json:"cust_notify_service_renewal"`

		SSHKeys  []SSHKey       `json:"sshkeys"`
		Passkeys []Passkey      `json:"passkeys"`
		Contacts []Contact      `json:"contacts"`
		Orders   []AccountOrder `json:"orders"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*a = Account{
		CustomerID:            r.CustomerID,
		Name:                  r.Name,
		CompanyNo:             r.CompanyNo,
		Address:               r.Address,
		Address2:              r.Address2,
		Province:              r.Province,
		ZipCode:               r.ZipCode,
		City:                  r.City,
		Country:               r.Country,
		Phone:                 r.Phone,
		Email:                 r.Email,
		BillingEmail:          r.BillingEmail,
		IsPartner:             bool(r.IsPartner),
		Newsletter:            r.Newsletter,
		Incident:              r.Incident,
		BandwidthNotification: r.BandwidthNotification,
		EmailOnLogin:          r.EmailOnLogin,
		NotifyServiceRenewal:  r.NotifyServiceRenewal,
		SSHKeys:               r.SSHKeys,
		Passkeys:              r.Passkeys,
		Contacts:              r.Contacts,
		Orders:                r.Orders,
	}

	return nil
}

// UpdateAddressRequest contains the editable billing/contact address fields
// for PUT /account. All fields are optional; send only what you want to change.
type UpdateAddressRequest struct {
	Address       string `json:"cust_address,omitzero"`
	Address2      string `json:"cust_address2,omitzero"`
	ZipCode       string `json:"cust_zipcode,omitzero"`
	City          string `json:"cust_city,omitzero"`
	Province      string `json:"cust_province,omitzero"`
	Country       string `json:"cust_country,omitzero"`
	BillingEmail  string `json:"cust_billing_email,omitzero"`
	BillingEmail2 string `json:"cust_billing_email2,omitzero"`
}

// NotificationPrefs contains all email notification toggle fields for
// PUT /account/notifications.
type NotificationPrefs struct {
	Newsletter            bool `json:"cust_newsletter"`
	Incident              bool `json:"cust_incident"`
	BandwidthNotification bool `json:"cust_bandwidth_notification"`
	EmailOnLogin          bool `json:"cust_email_on_login"`
	NotifyServiceRenewal  bool `json:"cust_notify_service_renewal"`
}

// Get returns the authenticated account via GET /account.
func (s *AccountService) Get(ctx context.Context) (*Account, error) {
	var out Account
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/account",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// UpdateAddress updates the billing/contact address fields on the account.
// Only fields with non-zero values are sent.
func (s *AccountService) UpdateAddress(ctx context.Context, req UpdateAddressRequest) error {
	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/account",
		body:   req,
	})

	return err
}

// UpdateNotifications updates the email notification preferences.
func (s *AccountService) UpdateNotifications(ctx context.Context, prefs NotificationPrefs) error {
	_, err := s.client.do(ctx, requestOptions{
		method: "PUT",
		path:   "/account/notifications",
		body:   prefs,
	})

	return err
}

// GetActivity returns recent account activity log entries.
func (s *AccountService) GetActivity(ctx context.Context) ([]ActivityLogEntry, error) {
	var out []ActivityLogEntry
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/account/activity",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// ChangePassword changes the authenticated user's own password.
func (s *AccountService) ChangePassword(ctx context.Context, current, newPassword string) error {
	if current == "" {
		return errors.New("gigahost: ChangePassword: current password is required")
	}

	if newPassword == "" {
		return errors.New("gigahost: ChangePassword: new password is required")
	}

	body := map[string]string{
		"current": current,
		"new":     newPassword,
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/account/password",
		body:   body,
	})

	return err
}

// AddSSHKey adds an SSH public key to the account.
func (s *AccountService) AddSSHKey(ctx context.Context, name, data string) error {
	if name == "" {
		return errors.New("gigahost: AddSSHKey: name is required")
	}

	if data == "" {
		return errors.New("gigahost: AddSSHKey: data is required")
	}

	body := map[string]string{
		"name": name,
		"data": data,
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/account/sshkey",
		body:   body,
	})

	return err
}

// DeleteSSHKey removes an SSH public key from the account.
func (s *AccountService) DeleteSSHKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return errors.New("gigahost: DeleteSSHKey: keyID is required")
	}

	_, err := s.client.do(ctx, requestOptions{
		method: "DELETE",
		path:   "/account/sshkey/" + url.PathEscape(keyID),
	})

	return err
}
