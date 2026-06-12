package client

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// DeployService groups the /deploy endpoints for creating and monitoring
// hourly-billed cloud VMs.
type DeployService struct {
	client *Client
}

// Product types as returned in the catalog's `type` field.
const (
	ProductTypeVM        = "vm"
	ProductTypeDedicated = "dedicated"
	ProductTypeAuction   = "auction"
)

// DeployDisk is one disk in a product's hardware specs.
type DeployDisk struct {
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"` // e.g. "NVMe"
}

// DeploySpecs is the structured hardware description of a product.
type DeploySpecs struct {
	CPUCores   int          `json:"cpu_cores"`
	CPUSockets int          `json:"cpu_sockets"`
	CPUModel   string       `json:"cpu_model"` // empty for VMs
	RAMGB      int          `json:"ram_gb"`
	Disks      []DeployDisk `json:"disks"`
	Bandwidth  int          `json:"bw"`
	BWType     string       `json:"bw_type"`
	UplinkGbps float64      `json:"uplink_gbps"` // 0 when the API reports null
}

// DeployProduct is one product (VM size) available for deployment.
type DeployProduct struct {
	ID   string
	Hash string
	Name string
	// Type is the platform discriminator: "vm", "dedicated", or "auction".
	Type string
	// PriceID is required by POST /deploy/servers; the catalog is the only
	// place it is exposed. It is 1:1 with the product.
	PriceID       string
	Cores         int
	MemoryGB      int
	StorageGB     int
	BandwidthGB   int
	BandwidthType string // e.g. "quota"
	Specs         DeploySpecs
	RateHourly    float64
	RateMonthly   float64
	RegionIDs     []string
}

// UnmarshalJSON maps snake_case API fields. The API returns the ID fields as
// bare numbers and the vm_* sizes as numeric strings, so we coerce both.
func (p *DeployProduct) UnmarshalJSON(data []byte) error {
	type rawSpecs struct {
		CPUCores   apiInt       `json:"cpu_cores"`
		CPUSockets apiInt       `json:"cpu_sockets"`
		CPUModel   apiString    `json:"cpu_model"`
		RAMGB      apiInt       `json:"ram_gb"`
		Disks      []DeployDisk `json:"disks"`
		Bandwidth  apiInt       `json:"bw"`
		BWType     string       `json:"bw_type"`
		UplinkGbps float64      `json:"uplink_gbps"`
	}

	type raw struct {
		ID            apiString   `json:"product_id"`
		Hash          string      `json:"product_hash"`
		Name          string      `json:"product_name"`
		Type          string      `json:"type"`
		PriceID       apiString   `json:"price_id"`
		Cores         apiInt      `json:"vm_cores"`
		MemoryGB      apiInt      `json:"vm_memory"`
		StorageGB     apiInt      `json:"vm_storage"`
		BandwidthGB   apiInt      `json:"vm_bw"`
		BandwidthType string      `json:"vm_bw_type"`
		Specs         rawSpecs    `json:"specs"`
		RateHourly    float64     `json:"rate_hourly"`
		RateMonthly   float64     `json:"rate_monthly"`
		RegionIDs     []apiString `json:"region_ids"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*p = DeployProduct{
		ID:            string(r.ID),
		Hash:          r.Hash,
		Name:          r.Name,
		Type:          r.Type,
		PriceID:       string(r.PriceID),
		Cores:         int(r.Cores),
		MemoryGB:      int(r.MemoryGB),
		StorageGB:     int(r.StorageGB),
		BandwidthGB:   int(r.BandwidthGB),
		BandwidthType: r.BandwidthType,
		Specs: DeploySpecs{
			CPUCores:   int(r.Specs.CPUCores),
			CPUSockets: int(r.Specs.CPUSockets),
			CPUModel:   string(r.Specs.CPUModel),
			RAMGB:      int(r.Specs.RAMGB),
			Disks:      r.Specs.Disks,
			Bandwidth:  int(r.Specs.Bandwidth),
			BWType:     r.Specs.BWType,
			UplinkGbps: r.Specs.UplinkGbps,
		},
		RateHourly:  r.RateHourly,
		RateMonthly: r.RateMonthly,
		RegionIDs:   apiStrings(r.RegionIDs),
	}

	return nil
}

// DeployTier groups products by category (e.g. "Standard", "Memory").
type DeployTier struct {
	GroupID   string
	GroupName string
	Products  []DeployProduct
}

// UnmarshalJSON maps snake_case API fields. group_id arrives as a bare number.
func (t *DeployTier) UnmarshalJSON(data []byte) error {
	type raw struct {
		GroupID   apiString       `json:"group_id"`
		GroupName string          `json:"group_name"`
		Products  []DeployProduct `json:"products"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*t = DeployTier{
		GroupID:   string(r.GroupID),
		GroupName: r.GroupName,
		Products:  r.Products,
	}

	return nil
}

// DeployRegion is a datacenter region where servers can be deployed.
type DeployRegion struct {
	ID        string
	Name      string // e.g. "Sandefjord"
	NameShort string // e.g. "SFJ, NO"
	Country   string // e.g. "Norge"
	Active    bool
}

// UnmarshalJSON maps snake_case API fields; region_active arrives as "1"/"0".
func (r *DeployRegion) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID        apiString `json:"region_id"`
		Name      string    `json:"region_name"`
		NameShort string    `json:"region_name_short"`
		Country   string    `json:"region_country"`
		Active    apiBool   `json:"region_active"`
	}

	var w raw
	if err := unmarshalJSON(data, &w); err != nil {
		return err
	}

	*r = DeployRegion{
		ID:        string(w.ID),
		Name:      w.Name,
		NameShort: w.NameShort,
		Country:   w.Country,
		Active:    bool(w.Active),
	}

	return nil
}

// DeployCatalog is the full response from GET /deploy/servers.
type DeployCatalog struct {
	Tiers    []DeployTier
	Regions  []DeployRegion
	Currency string
}

// UnmarshalJSON maps the catalog envelope.
func (c *DeployCatalog) UnmarshalJSON(data []byte) error {
	type raw struct {
		Tiers    []DeployTier   `json:"tiers"`
		Regions  []DeployRegion `json:"regions"`
		Currency string         `json:"currency"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*c = DeployCatalog(r)

	return nil
}

// DeployServerRequest is the body for POST /deploy/servers.
// Exactly one of OSID, ISOID, or Rescue must be set.
// At least one of ProductID or ProductHash must be set.
type DeployServerRequest struct {
	// ProductID is the product_id from the catalog (pid in the API body).
	// Mutually exclusive with ProductHash; at least one is required.
	ProductID string

	// ProductHash is the product_hash from the catalog.
	// Mutually exclusive with ProductID; at least one is required.
	ProductHash string

	// PriceID is required by the API.
	PriceID string

	// RegionID is required.
	RegionID string

	// Exactly one of OSID, ISOID, or Rescue must be set.
	OSID   string
	ISOID  string
	Rescue bool

	// Optional fields.
	Quantity  int
	Backups   bool
	Hostnames []string
	SSHKeys   []string // key IDs
	Opts      map[string]any
}

// deployRequestWire is the wire-format body for POST /deploy/servers.
type deployRequestWire struct {
	ProductID   string         `json:"pid,omitzero"`
	ProductHash string         `json:"hash,omitzero"`
	PriceID     string         `json:"price_id"`
	RegionID    string         `json:"region_id"`
	OSID        string         `json:"os_id,omitzero"`
	ISOID       string         `json:"iso_id,omitzero"`
	Rescue      bool           `json:"rescue,omitzero"`
	Quantity    int            `json:"quantity,omitzero"`
	Backups     int            `json:"backups,omitzero"` // 0 or 1
	Hostnames   []string       `json:"hostnames,omitzero"`
	SSHKeys     []string       `json:"ssh_keys,omitzero"`
	Opts        map[string]any `json:"opts,omitzero"`
}

// DeployServerResponse is the response from POST /deploy/servers.
type DeployServerResponse struct {
	OrderIDs     []string
	OrderNumbers []string
	Quantity     int
	RateHourly   string
	MonthlyCap   string
	Currency     string
}

// UnmarshalJSON maps snake_case API fields. order_ids/order_numbers arrive as
// JSON numbers, so they are coerced to strings.
func (r *DeployServerResponse) UnmarshalJSON(data []byte) error {
	type raw struct {
		OrderIDs     []apiString `json:"order_ids"`
		OrderNumbers []apiString `json:"order_numbers"`
		Quantity     apiInt      `json:"quantity"`
		RateHourly   apiString   `json:"rate_hourly"`
		MonthlyCap   apiString   `json:"monthly_cap"`
		Currency     string      `json:"currency"`
	}

	var w raw
	if err := unmarshalJSON(data, &w); err != nil {
		return err
	}

	*r = DeployServerResponse{
		OrderIDs:     apiStrings(w.OrderIDs),
		OrderNumbers: apiStrings(w.OrderNumbers),
		Quantity:     int(w.Quantity),
		RateHourly:   string(w.RateHourly),
		MonthlyCap:   string(w.MonthlyCap),
		Currency:     w.Currency,
	}

	return nil
}

// DeployProvisionStatus enumerates the statuses returned by /deploy/status.
type DeployProvisionStatus string

const (
	DeployStatusWaiting    DeployProvisionStatus = "waiting"
	DeployStatusDeploying  DeployProvisionStatus = "deploying"
	DeployStatusInstalling DeployProvisionStatus = "installing"
	DeployStatusReady      DeployProvisionStatus = "ready"
	DeployStatusRescue     DeployProvisionStatus = "rescue"
	DeployStatusISO        DeployProvisionStatus = "iso"
)

// DeployServerStatus is the per-server entry in the /deploy/status response.
type DeployServerStatus struct {
	OrderID     string
	OrderNumber string
	Hostname    string
	ServerID    string
	IP          string
	IPv6        string
	Status      DeployProvisionStatus
	Password    string
}

// UnmarshalJSON maps snake_case API fields.
func (s *DeployServerStatus) UnmarshalJSON(data []byte) error {
	type raw struct {
		OrderID     apiString `json:"order_id"`
		OrderNumber apiString `json:"order_number"`
		Hostname    string    `json:"hostname"`
		ServerID    apiString `json:"srv_id"`
		IP          string    `json:"ip"`
		IPv6        string    `json:"ipv6"`
		Status      string    `json:"status"`
		Password    string    `json:"password"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*s = DeployServerStatus{
		OrderID:     string(r.OrderID),
		OrderNumber: string(r.OrderNumber),
		Hostname:    r.Hostname,
		ServerID:    string(r.ServerID),
		IP:          r.IP,
		IPv6:        r.IPv6,
		Status:      DeployProvisionStatus(r.Status),
		Password:    r.Password,
	}

	return nil
}

// DeployStatus is the full response from GET /deploy/status.
type DeployStatus struct {
	Servers  []DeployServerStatus
	AllReady bool
}

// UnmarshalJSON maps the status envelope.
func (s *DeployStatus) UnmarshalJSON(data []byte) error {
	type raw struct {
		Servers  []DeployServerStatus `json:"servers"`
		AllReady bool                 `json:"all_ready"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*s = DeployStatus(r)

	return nil
}

// DeployISO is one ISO available for deployment.
type DeployISO struct {
	ID   string
	Name string
	Size int64
}

// UnmarshalJSON maps snake_case API fields.
func (i *DeployISO) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID   string `json:"iso_id"`
		Name string `json:"iso_name"`
		Size apiInt `json:"iso_size"`
	}

	var r raw
	if err := unmarshalJSON(data, &r); err != nil {
		return err
	}

	*i = DeployISO{
		ID:   r.ID,
		Name: r.Name,
		Size: int64(r.Size),
	}

	return nil
}

// GetCatalog returns the full cloud server catalog including tiers,
// products, regions, and currency.
func (s *DeployService) GetCatalog(ctx context.Context) (*DeployCatalog, error) {
	var out DeployCatalog
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/deploy/servers",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// Deploy creates one or more hourly-billed servers. Exactly one of
// req.OSID, req.ISOID, or req.Rescue must be set. At least one of
// req.ProductID or req.ProductHash must be provided.
func (s *DeployService) Deploy(ctx context.Context, req DeployServerRequest) (*DeployServerResponse, error) {
	if req.ProductID == "" && req.ProductHash == "" {
		return nil, errors.New("gigahost: Deploy: one of ProductID or ProductHash is required")
	}

	if req.RegionID == "" {
		return nil, errors.New("gigahost: Deploy: RegionID is required")
	}

	if req.PriceID == "" {
		return nil, errors.New("gigahost: Deploy: PriceID is required (from the catalog product)")
	}

	osSet := 0
	if req.OSID != "" {
		osSet++
	}

	if req.ISOID != "" {
		osSet++
	}

	if req.Rescue {
		osSet++
	}

	if osSet != 1 {
		return nil, errors.New("gigahost: Deploy: exactly one of OSID, ISOID, or Rescue must be set")
	}

	backups := 0
	if req.Backups {
		backups = 1
	}

	wire := deployRequestWire{
		ProductID:   req.ProductID,
		ProductHash: req.ProductHash,
		PriceID:     req.PriceID,
		RegionID:    req.RegionID,
		OSID:        req.OSID,
		ISOID:       req.ISOID,
		Rescue:      req.Rescue,
		Quantity:    req.Quantity,
		Backups:     backups,
		Hostnames:   req.Hostnames,
		SSHKeys:     req.SSHKeys,
		Opts:        req.Opts,
	}

	var out DeployServerResponse
	if _, err := s.client.do(ctx, requestOptions{
		method: "POST",
		path:   "/deploy/servers",
		body:   wire,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// GetStatus returns the provisioning status for the given order IDs.
// Poll this endpoint until DeployStatus.AllReady is true.
func (s *DeployService) GetStatus(ctx context.Context, orderIDs []string) (*DeployStatus, error) {
	if len(orderIDs) == 0 {
		return nil, errors.New("gigahost: Deploy.GetStatus: at least one order ID is required")
	}

	q := url.Values{"ids": []string{strings.Join(orderIDs, ",")}}

	var out DeployStatus
	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/deploy/status",
		query:  q,
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// ListISOs returns the uploaded ISOs available for deployment.
//
// The live API wraps the list in an object: `data: {"isos": [...]}`.
func (s *DeployService) ListISOs(ctx context.Context) ([]DeployISO, error) {
	var out struct {
		ISOs []DeployISO `json:"isos"`
	}

	if _, err := s.client.do(ctx, requestOptions{
		method: "GET",
		path:   "/deploy/isos",
		dst:    &out,
	}); err != nil {
		return nil, err
	}

	return out.ISOs, nil
}
