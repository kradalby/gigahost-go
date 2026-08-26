package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kradalby/gigahost-go/client"
)

func TestDeployGetCatalog(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/deploy/servers").
		RespondFixture(t, "testdata/deploy/catalog.json")

	catalog, err := c.Deploy.GetCatalog(context.Background())
	if err != nil {
		t.Fatalf("Deploy.GetCatalog: %v", err)
	}

	if len(catalog.Tiers) != 4 {
		t.Fatalf("want 4 tiers, got %d", len(catalog.Tiers))
	}

	tier := catalog.Tiers[0]

	if tier.GroupName != "KVM Performance" {
		t.Errorf("GroupName = %q", tier.GroupName)
	}

	if len(tier.Products) != 2 {
		t.Fatalf("want 2 products, got %d", len(tier.Products))
	}

	p := tier.Products[0]

	if p.ID != "7951" {
		t.Errorf("Product.ID = %q", p.ID)
	}

	if p.Hash != "f9544dd3cb" {
		t.Errorf("Product.Hash = %q", p.Hash)
	}

	if p.Type != client.ProductTypeVM {
		t.Errorf("Product.Type = %q, want vm", p.Type)
	}

	if p.PriceID != "4042" {
		t.Errorf("Product.PriceID = %q, want 4042", p.PriceID)
	}

	if p.Cores != 2 {
		t.Errorf("Cores = %d, want 2", p.Cores)
	}

	if p.MemoryGB != 4 {
		t.Errorf("MemoryGB = %d, want 4", p.MemoryGB)
	}

	if p.RateHourly != 0.15813 {
		t.Errorf("RateHourly = %v, want 0.15813", p.RateHourly)
	}

	if len(p.RegionIDs) != 1 {
		t.Errorf("RegionIDs = %v", p.RegionIDs)
	}

	// Structured specs: VM product has no CPU model, one NVMe disk.
	if p.Specs.CPUCores != 2 || p.Specs.RAMGB != 4 {
		t.Errorf("Specs = %+v, want cpu_cores 2 / ram_gb 4", p.Specs)
	}

	if p.Specs.CPUModel != "" {
		t.Errorf("Specs.CPUModel = %q, want empty (null)", p.Specs.CPUModel)
	}

	if len(p.Specs.Disks) != 1 || p.Specs.Disks[0].SizeGB != 40 || p.Specs.Disks[0].Type != "NVMe" {
		t.Errorf("Specs.Disks = %+v, want one 40GB NVMe", p.Specs.Disks)
	}

	// Dedicated product carries a CPU model and uplink.
	metal := catalog.Tiers[2].Products[0]

	if metal.Type != client.ProductTypeDedicated {
		t.Errorf("metal.Type = %q, want dedicated", metal.Type)
	}

	if metal.Specs.CPUModel != "Intel Core i5-2400 3.1GHz" {
		t.Errorf("metal.Specs.CPUModel = %q", metal.Specs.CPUModel)
	}

	if metal.Specs.UplinkGbps != 1 {
		t.Errorf("metal.Specs.UplinkGbps = %v, want 1", metal.Specs.UplinkGbps)
	}

	// Auction product: type auction, price_id 0 (not hourly-deployable).
	auction := catalog.Tiers[3].Products[0]

	if auction.Type != client.ProductTypeAuction {
		t.Errorf("auction.Type = %q, want auction", auction.Type)
	}

	if auction.PriceID != "0" {
		t.Errorf("auction.PriceID = %q, want 0", auction.PriceID)
	}

	if len(catalog.Regions) != 1 {
		t.Fatalf("want 1 region, got %d", len(catalog.Regions))
	}

	region := catalog.Regions[0]

	if region.Name != "Sandefjord" {
		t.Errorf("Region.Name = %q", region.Name)
	}

	if region.NameShort != "SFJ, NO" {
		t.Errorf("Region.NameShort = %q", region.NameShort)
	}

	if region.Country != "Norge" {
		t.Errorf("Region.Country = %q", region.Country)
	}

	if !region.Active {
		t.Error("Region.Active = false, want true")
	}

	if catalog.Currency != "NOK" {
		t.Errorf("Currency = %q", catalog.Currency)
	}
}

func TestDeployDeploy(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/deploy/servers").
		WithJSON(`{"pid":"42","price_id":"4054","region_id":"1","os_id":"72","quantity":2,"backups":1,"hostnames":["web01","web02"],"ssh_keys":["5"]}`).
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{
				"order_ids":["100","101"],
				"order_numbers":["GH-100001","GH-100002"],
				"quantity":2,
				"rate_hourly":"0.05",
				"monthly_cap":"30.00",
				"currency":"NOK"
			}
		}`)

	resp, err := c.Deploy.Deploy(context.Background(), client.DeployServerRequest{
		ProductID: "42",
		PriceID:   "4054",
		RegionID:  "1",
		OSID:      "72",
		Quantity:  2,
		Backups:   true,
		Hostnames: []string{"web01", "web02"},
		SSHKeys:   []string{"5"},
	})
	if err != nil {
		t.Fatalf("Deploy.Deploy: %v", err)
	}

	if len(resp.OrderIDs) != 2 {
		t.Fatalf("want 2 order IDs, got %d", len(resp.OrderIDs))
	}

	if resp.OrderIDs[0] != "100" {
		t.Errorf("OrderIDs[0] = %q", resp.OrderIDs[0])
	}

	if resp.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", resp.Quantity)
	}

	if resp.Currency != "NOK" {
		t.Errorf("Currency = %q", resp.Currency)
	}
}

func TestDeployGetStatus(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/deploy/status").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{
				"servers":[
					{
						"order_id":"100",
						"order_number":"GH-100001",
						"hostname":"web01.gigahost.no",
						"srv_id":"3600",
						"ip":"185.181.63.100",
						"ipv6":"2a03:94e0::1",
						"status":"ready",
						"password":"s3cr3t"
					}
				],
				"all_ready":true
			}
		}`)

	status, err := c.Deploy.GetStatus(context.Background(), []string{"100"})
	if err != nil {
		t.Fatalf("Deploy.GetStatus: %v", err)
	}

	if !status.AllReady {
		t.Error("AllReady should be true")
	}

	if len(status.Servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(status.Servers))
	}

	s := status.Servers[0]

	if s.OrderID != "100" {
		t.Errorf("OrderID = %q", s.OrderID)
	}

	if s.ServerID != "3600" {
		t.Errorf("ServerID = %q", s.ServerID)
	}

	if s.Status != client.DeployStatusReady {
		t.Errorf("Status = %q, want ready", s.Status)
	}

	if s.Password != "s3cr3t" {
		t.Errorf("Password = %q", s.Password)
	}

	if s.IP != "185.181.63.100" {
		t.Errorf("IP = %q", s.IP)
	}
}

func TestDeployListISOs(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	// Live shape: the list is wrapped in an object (caught by the CLI
	// smoke suite against the real API).
	srv.Expect("GET", "/deploy/isos").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{"isos":[
				{"iso_id":"7","iso_name":"ubuntu-24.04.iso","iso_size":1073741824}
			]}
		}`)

	isos, err := c.Deploy.ListISOs(context.Background())
	if err != nil {
		t.Fatalf("Deploy.ListISOs: %v", err)
	}

	if len(isos) != 1 {
		t.Fatalf("want 1 ISO, got %d", len(isos))
	}

	if isos[0].ID != "7" {
		t.Errorf("ID = %q", isos[0].ID)
	}

	if isos[0].Name != "ubuntu-24.04.iso" {
		t.Errorf("Name = %q", isos[0].Name)
	}

	if isos[0].Size != 1073741824 {
		t.Errorf("Size = %d", isos[0].Size)
	}
}

func TestDeployValidation(t *testing.T) {
	t.Parallel()

	_, c := newServerAndClient(t)

	ctx := context.Background()

	// No product ID or hash.
	_, err := c.Deploy.Deploy(ctx, client.DeployServerRequest{
		RegionID: "1",
		OSID:     "72",
	})
	if err == nil {
		t.Error("expected error when no ProductID or ProductHash")
	}

	// No region.
	_, err = c.Deploy.Deploy(ctx, client.DeployServerRequest{
		ProductID: "42",
		OSID:      "72",
	})
	if err == nil {
		t.Error("expected error when no RegionID")
	}

	// No price ID.
	_, err = c.Deploy.Deploy(ctx, client.DeployServerRequest{
		ProductID: "42",
		RegionID:  "1",
		OSID:      "72",
	})
	if err == nil {
		t.Error("expected error when PriceID is empty")
	}

	// No OS / ISO / rescue.
	_, err = c.Deploy.Deploy(ctx, client.DeployServerRequest{
		ProductID: "42",
		PriceID:   "4054",
		RegionID:  "1",
	})
	if err == nil {
		t.Error("expected error when none of OSID/ISOID/Rescue set")
	}

	// More than one of OS / ISO / rescue.
	_, err = c.Deploy.Deploy(ctx, client.DeployServerRequest{
		ProductID: "42",
		RegionID:  "1",
		OSID:      "72",
		Rescue:    true,
	})
	if err == nil {
		t.Error("expected error when more than one of OSID/ISOID/Rescue set")
	}

	// No order IDs for status.
	_, err = c.Deploy.GetStatus(ctx, nil)
	if err == nil {
		t.Error("expected error when no order IDs given to GetStatus")
	}
}
