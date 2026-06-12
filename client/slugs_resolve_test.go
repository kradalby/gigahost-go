package client_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestReinstallResolveOS(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/reinstall/distro").
		RespondFixture(t, "testdata/reinstall/distros.json")
	srv.Expect("GET", "/reinstall/distro/2").
		RespondFixture(t, "testdata/reinstall/distro_debian.json")
	srv.Expect("GET", "/reinstall/distro/3").
		RespondFixture(t, "testdata/reinstall/distro_ubuntu.json")

	got, err := c.Reinstall.ResolveOS(context.Background(), "noble")
	if err != nil {
		t.Fatalf("ResolveOS(noble): %v", err)
	}

	if got.OS.ID != "102" {
		t.Errorf("ResolveOS(noble) = os %s, want 102", got.OS.ID)
	}

	if got.Slug != "ubuntu-24.04" {
		t.Errorf("Slug = %q, want ubuntu-24.04", got.Slug)
	}

	if got.Distribution.Value != "ubuntu" {
		t.Errorf("Distribution.Value = %q, want ubuntu", got.Distribution.Value)
	}
}

func TestServersResolveByHostname(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers").
		RespondFixture(t, "testdata/servers/list.json")

	got, err := c.Servers.Resolve(context.Background(), "SRV3523.gigahost.no")
	if err != nil {
		t.Fatalf("Resolve(hostname): %v", err)
	}

	if got.ID != "3523" {
		t.Errorf("Resolve(hostname) = server %s, want 3523", got.ID)
	}
}

func TestServersResolveUnknownHostname(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers").
		RespondFixture(t, "testdata/servers/list.json")

	_, err := c.Servers.Resolve(context.Background(), "nope.example.no")
	if err == nil {
		t.Fatal("Resolve(unknown): want error")
	}

	if !strings.Contains(err.Error(), "srv3523.gigahost.no") {
		t.Errorf("error %q does not list known hostnames", err)
	}
}

func TestDNSResolveZoneByName(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/zones").
		RespondFixture(t, "testdata/dns/list_zones.json")

	got, err := c.DNS.ResolveZone(context.Background(), "EXAMPLE.no")
	if err != nil {
		t.Fatalf("ResolveZone(name): %v", err)
	}

	if got.ID != "123" {
		t.Errorf("ResolveZone(name) = zone %s, want 123", got.ID)
	}
}

func TestDNSResolveZoneByID(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/dns/zones").
		RespondFixture(t, "testdata/dns/list_zones.json")

	got, err := c.DNS.ResolveZone(context.Background(), "123")
	if err != nil {
		t.Fatalf("ResolveZone(id): %v", err)
	}

	if got.Name != "example.no" {
		t.Errorf("ResolveZone(id) = zone %q, want example.no", got.Name)
	}
}

func TestDeployResolveISO(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/deploy/isos").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{"isos":[
				{"iso_id":"abc123","iso_name":"custom-installer.iso","iso_size":734003200},
				{"iso_id":"def456","iso_name":"rescue-tools.iso","iso_size":104857600}
			]}
		}`)

	got, err := c.Deploy.ResolveISO(context.Background(), "custom-installer")
	if err != nil {
		t.Fatalf("ResolveISO: %v", err)
	}

	if got.ID != "abc123" {
		t.Errorf("ResolveISO = %s, want abc123", got.ID)
	}
}

func TestDeployResolveISONotFound(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/deploy/isos").
		Respond(http.StatusOK, `{
			"meta":{"status":200,"status_message":"200 OK"},
			"data":{"isos":[{"iso_id":"abc123","iso_name":"custom-installer.iso","iso_size":1}]}
		}`)

	_, err := c.Deploy.ResolveISO(context.Background(), "windows.iso")
	if err == nil {
		t.Fatal("ResolveISO(unknown): want error")
	}

	if !strings.Contains(err.Error(), "custom-installer.iso") {
		t.Errorf("error %q does not list available ISOs", err)
	}
}

func TestAccountResolveSSHKeys(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/account").
		RespondFixture(t, "testdata/account/get.json")

	ids, err := c.Account.ResolveSSHKeys(context.Background(), []string{"laptop", "5"})
	if err != nil {
		t.Fatalf("ResolveSSHKeys: %v", err)
	}

	if len(ids) != 2 || ids[0] != "5" || ids[1] != "5" {
		t.Errorf("ResolveSSHKeys = %v, want [5 5]", ids)
	}
}

func TestAccountResolveSSHKeysUnknown(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/account").
		RespondFixture(t, "testdata/account/get.json")

	_, err := c.Account.ResolveSSHKeys(context.Background(), []string{"desktop"})
	if err == nil {
		t.Fatal("ResolveSSHKeys(unknown): want error")
	}

	if !strings.Contains(err.Error(), "laptop") {
		t.Errorf("error %q does not list known key names", err)
	}
}
