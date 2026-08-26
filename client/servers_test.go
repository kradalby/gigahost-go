package client_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kradalby/gigahost-go/client"
)

func TestServersCancel(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/servers/17482/cancel").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"Server has been cancelled."}}`)

	if err := c.Servers.Cancel(context.Background(), "17482"); err != nil {
		t.Fatalf("Servers.Cancel: %v", err)
	}

	if err := c.Servers.Cancel(context.Background(), ""); err == nil {
		t.Error("expected error for empty serverID")
	}
}

func TestServersList(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers").
		RespondFixture(t, "testdata/servers/list.json")

	ctx := context.Background()

	servers, err := c.Servers.List(ctx)
	if err != nil {
		t.Fatalf("Servers.List: %v", err)
	}

	if len(servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(servers))
	}

	s := servers[0]

	if s.ID != "3523" {
		t.Errorf("ID = %q", s.ID)
	}

	if s.Hostname != "srv3523.gigahost.no" {
		t.Errorf("Hostname = %q", s.Hostname)
	}

	if !s.Status {
		t.Error("Status should be true")
	}

	if s.Cores != 2 {
		t.Errorf("Cores = %d, want 2", s.Cores)
	}

	if s.Bandwidth != 1000 {
		t.Errorf("Bandwidth = %d", s.Bandwidth)
	}

	if !s.FeatureReinstall {
		t.Error("FeatureReinstall should be true")
	}

	wantCreated := time.Unix(1530609706, 0).UTC()
	if !s.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt = %v, want %v", s.CreatedAt, wantCreated)
	}

	if len(s.IPs) != 1 {
		t.Fatalf("want 1 IP, got %d", len(s.IPs))
	}

	if s.IPs[0].Address != "185.181.63.24" {
		t.Errorf("IP = %q", s.IPs[0].Address)
	}

	if s.OS == nil || s.OS.Name != "Ubuntu 18.04 LTS 64-bit" {
		t.Errorf("OS = %+v", s.OS)
	}
}

func TestServersPowerLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers/3523/powerstate").
		Respond(http.StatusOK, `{"success":true,"meta":{"status":200,"status_message":"200 OK"},"powerstate":true,"timestamp":1530706429}`)

	srv.Expect("GET", "/servers/3523/power/off").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	srv.Expect("GET", "/servers/3523/power/on").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	srv.Expect("GET", "/servers/3523/reboot").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	state, err := c.Servers.GetPowerState(ctx, "3523")
	if err != nil {
		t.Fatalf("GetPowerState: %v", err)
	}

	if !state.PowerState {
		t.Error("want PowerState=true")
	}

	if state.Timestamp.IsZero() {
		t.Error("want non-zero Timestamp")
	}

	if err := c.Servers.PowerOff(ctx, "3523"); err != nil {
		t.Fatalf("PowerOff: %v", err)
	}

	if err := c.Servers.PowerOn(ctx, "3523"); err != nil {
		t.Fatalf("PowerOn: %v", err)
	}

	if err := c.Servers.Reboot(ctx, "3523"); err != nil {
		t.Fatalf("Reboot: %v", err)
	}
}

func TestServersUpdateName(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/servers/3523/name").
		WithJSON(`{"name":"web-prod"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.Servers.UpdateName(context.Background(), "3523", "web-prod"); err != nil {
		t.Fatalf("UpdateName: %v", err)
	}
}

func TestServersOrderIPv4(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/servers/3523/ipv4").
		WithJSON(`{"ip_type":"l3"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	if err := c.Servers.OrderIPv4(context.Background(), "3523", client.IPTypeL3); err != nil {
		t.Fatalf("OrderIPv4: %v", err)
	}
}

func TestSnapshotsLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/servers/3523/snapshots").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":[{"snap_id":42,"srv_id":3523,"snap_name":"Asdf1234","snap_display_name":"backup-1","snap_time":1700000000,"snap_state":"completed"}]}`)

	srv.Expect("POST", "/servers/3523/snapshot").
		WithJSON(`{"name":"backup-2"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"Snapshot is currently being created."}}`)

	srv.Expect("DELETE", "/servers/3523/snapshot/42").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	ctx := context.Background()

	snaps, err := c.Snapshots.List(ctx, "3523")
	if err != nil {
		t.Fatalf("Snapshots.List: %v", err)
	}

	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}

	if snaps[0].State != client.SnapshotStateCompleted {
		t.Errorf("State = %q", snaps[0].State)
	}

	if err := c.Snapshots.Create(ctx, "3523", "backup-2"); err != nil {
		t.Fatalf("Snapshots.Create: %v", err)
	}

	if err := c.Snapshots.Delete(ctx, "3523", 42); err != nil {
		t.Fatalf("Snapshots.Delete: %v", err)
	}
}

func TestReinstallLifecycle(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("GET", "/reinstall/distro").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":[{"dist_id":"1","type_id":"1","dist_name":"Debian","dist_value":"debian","dist_logo":"/images/os/debian.png","dist_description":"","dist_active":"1"}]}`)

	srv.Expect("GET", "/reinstall/distro/1").
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":[{"os_id":"72","dist_id":"1","os_name":"Debian 12 64-bit","os_release":"debian","os_dist":"12","os_arch":"amd64","os_custom_partition":"1","os_single_disk_only":"0","os_support_raid":"1","os_dedicated_only":"0","os_minram":"0"}]}`)

	srv.Expect("POST", "/servers/3523/reinstall").
		WithJSON(`{"os_id":"72","language":"en_US","keyboard":"no","timezone":"Europe/Oslo","hostname":"srv3523.gigahost.no"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK","message":"Server install has been initiated."},"reboot":true,"root_passwd":"sup3rs3cret"}`)

	ctx := context.Background()

	distros, err := c.Reinstall.ListDistributions(ctx)
	if err != nil {
		t.Fatalf("ListDistributions: %v", err)
	}

	if len(distros) != 1 || distros[0].Value != "debian" {
		t.Errorf("distros[0].Value = %q", distros[0].Value)
	}

	if !distros[0].Active {
		t.Error("Active should be true")
	}

	oses, err := c.Reinstall.ListOperatingSystems(ctx, "1")
	if err != nil {
		t.Fatalf("ListOperatingSystems: %v", err)
	}

	if len(oses) != 1 || oses[0].Name != "Debian 12 64-bit" {
		t.Errorf("oses[0].Name = %q", oses[0].Name)
	}

	result, err := c.Reinstall.Reinstall(ctx, "3523", client.ReinstallRequest{
		OSID:     "72",
		Language: "en_US",
		Keyboard: "no",
		Timezone: "Europe/Oslo",
		Hostname: "srv3523.gigahost.no",
	})
	if err != nil {
		t.Fatalf("Reinstall: %v", err)
	}

	if !result.Reboot {
		t.Error("want Reboot=true")
	}

	if result.RootPasswd != "sup3rs3cret" {
		t.Errorf("RootPasswd = %q", result.RootPasswd)
	}
}

func TestIPMICreate(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("POST", "/servers/3523/ipmi").
		WithJSON(`{"acl":"1.2.3.4;10.0.0.0/24"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"},"data":{"kvm_ip_address":"185.125.168.1","username":"kvm-user","password":"kvm-pass"}}`)

	sess, err := c.IPMI.Create(context.Background(), "3523", "1.2.3.4;10.0.0.0/24")
	if err != nil {
		t.Fatalf("IPMI.Create: %v", err)
	}

	if sess.IPAddress != "185.125.168.1" {
		t.Errorf("IPAddress = %q", sess.IPAddress)
	}
}

func TestServersUpdateReverse(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	srv.Expect("PUT", "/servers/3523/reverse").
		WithJSON(`{"ip_id":"9001","dns":"mail.example.no"}`).
		Respond(http.StatusOK, `{"meta":{"status":200,"status_message":"200 OK"}}`)

	err := c.Servers.UpdateReverse(context.Background(), "3523", client.UpdateReverseRequest{
		IPID: "9001",
		DNS:  "mail.example.no",
	})
	if err != nil {
		t.Fatalf("UpdateReverse: %v", err)
	}

	if err := c.Servers.UpdateReverse(context.Background(), "", client.UpdateReverseRequest{IPID: "9001", DNS: "x.no"}); err == nil {
		t.Error("expected error for empty serverID")
	}

	if err := c.Servers.UpdateReverse(context.Background(), "3523", client.UpdateReverseRequest{IPID: "9001"}); err == nil {
		t.Error("expected error for empty DNS")
	}

	if err := c.Servers.UpdateReverse(context.Background(), "3523", client.UpdateReverseRequest{DNS: "x.no"}); err == nil {
		t.Error("expected error when both IPID and SubnetID are empty")
	}
}

// Graph payloads synthesized from the API docs (base64-encoded PNGs);
// no captured live fixture exists because the test account's hourly VPS
// returns graphs only after sustained traffic.
func TestServersGetGraphs(t *testing.T) {
	t.Parallel()

	srv, c := newServerAndClient(t)

	const graphBody = `{"meta":{"status":200,"status_message":"200 OK"},"data":{"graph_day":"aGVsbG8=","graph_week":"d29ybGQ=","graph_month":"","graph_year":""}}`

	srv.Expect("GET", "/servers/3523/port_bits").
		Respond(http.StatusOK, graphBody)

	bw, err := c.Servers.GetBandwidthGraphs(context.Background(), "3523")
	if err != nil {
		t.Fatalf("GetBandwidthGraphs: %v", err)
	}

	if bw.GraphDay != "aGVsbG8=" {
		t.Errorf("GraphDay = %q", bw.GraphDay)
	}

	srv.Expect("GET", "/servers/3523/port_upkts").
		Respond(http.StatusOK, graphBody)

	pk, err := c.Servers.GetPacketGraphs(context.Background(), "3523")
	if err != nil {
		t.Fatalf("GetPacketGraphs: %v", err)
	}

	if pk.GraphWeek != "d29ybGQ=" {
		t.Errorf("GraphWeek = %q", pk.GraphWeek)
	}

	if _, err := c.Servers.GetBandwidthGraphs(context.Background(), ""); err == nil {
		t.Error("expected error for empty serverID")
	}
}
