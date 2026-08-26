package tfprovider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func TestStatusForOrder(t *testing.T) {
	st := &gigahost.DeployStatus{Servers: []gigahost.DeployServerStatus{
		{OrderID: "1", ServerID: "a"},
		{OrderID: "2", ServerID: "b"},
	}}

	if got := statusForOrder(st, "2"); got == nil || got.ServerID != "b" {
		t.Fatalf("statusForOrder(2) = %+v, want ServerID b", got)
	}

	if got := statusForOrder(st, "9"); got != nil {
		t.Fatalf("statusForOrder(9) = %+v, want nil", got)
	}

	if got := statusForOrder(nil, "1"); got != nil {
		t.Fatalf("statusForOrder(nil) = %+v, want nil", got)
	}
}

func TestStatusIsFailure(t *testing.T) {
	tests := map[gigahost.DeployProvisionStatus]bool{
		"":                                          false, // unknown-yet, not a failure
		gigahost.DeployStatusWaiting:                false,
		gigahost.DeployStatusDeploying:              false,
		gigahost.DeployStatusInstalling:             false,
		gigahost.DeployStatusReady:                  false,
		gigahost.DeployStatusRescue:                 false,
		gigahost.DeployStatusISO:                    false,
		gigahost.DeployProvisionStatus("error"):     true,
		gigahost.DeployProvisionStatus("failed"):    true,
		gigahost.DeployProvisionStatus("cancelled"): true,
	}

	for status, want := range tests {
		if got := statusIsFailure(status); got != want {
			t.Errorf("statusIsFailure(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestResultFromStatus(t *testing.T) {
	e := gigahost.DeployServerStatus{
		ServerID: "s1",
		IP:       "192.0.2.1",
		IPv6:     "2001:db8::1",
		Password: "hunter2",
		Status:   gigahost.DeployStatusReady,
	}

	got := resultFromStatus(e)
	if got.serverID != "s1" || got.ip != "192.0.2.1" || got.ipv6 != "2001:db8::1" ||
		got.password != "hunter2" {
		t.Fatalf("resultFromStatus = %+v", got)
	}
}

func TestServerIsReady(t *testing.T) {
	tests := []struct {
		name string
		srv  *gigahost.Server
		want bool
	}{
		{"nil", nil, false},
		{"installing", &gigahost.Server{StatusInstall: true, Status: true}, false},
		{"running", &gigahost.Server{Status: true}, true},
		{"rescue", &gigahost.Server{StatusRescue: true}, true},
		{"off", &gigahost.Server{}, false},
	}

	for _, tt := range tests {
		if got := serverIsReady(tt.srv); got != tt.want {
			t.Errorf("serverIsReady(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMergeServerIntoResult(t *testing.T) {
	res := &deployResult{}
	mergeServerIntoResult(res, &gigahost.Server{
		PrimaryIP: "192.0.2.5",
		Status:    true,
		IPs: []gigahost.ServerIP{
			{Address: "192.0.2.5"},
			{Address: "2001:db8::5"},
		},
	})

	if res.ip != "192.0.2.5" || res.ipv6 != "2001:db8::5" {
		t.Fatalf("mergeServerIntoResult = %+v", res)
	}
}

func TestRAMGB(t *testing.T) {
	tests := map[int]int{
		2:     2,    // GB as reported (live)
		4:     4,    // GB
		8:     8,    // GB
		1024:  1,    // MB mistakenly reported -> 1 GB
		4096:  4,    // MB -> 4 GB
		16384: 16,   // MB -> 16 GB
		48:    48,   // GB, not a 1024 multiple
		1500:  1500, // large but not a 1024 multiple: left as-is
	}

	for in, want := range tests {
		if got := ramGB(in); got != want {
			t.Errorf("ramGB(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestStringOrNull(t *testing.T) {
	if v := stringOrNull(""); !v.IsNull() {
		t.Errorf("stringOrNull(\"\") = %v, want null", v)
	}

	if v := stringOrNull("x"); v.ValueString() != "x" {
		t.Errorf("stringOrNull(x) = %v, want x", v)
	}
}

func TestNullUnknownRuntime(t *testing.T) {
	m := serverResourceModel{
		IP:          types.StringUnknown(),
		IPv6:        types.StringUnknown(),
		Password:    types.StringValue("keep"),
		Status:      types.StringUnknown(),
		PrimaryIPID: types.StringUnknown(),
		Cores:       types.Int64Unknown(),
	}

	m.nullUnknownRuntime()

	if !m.IP.IsNull() || !m.IPv6.IsNull() || !m.Status.IsNull() || !m.PrimaryIPID.IsNull() {
		t.Errorf("unknown strings not nulled: %+v", m)
	}

	if m.Password.ValueString() != "keep" {
		t.Errorf("known password overwritten: %v", m.Password)
	}

	if !m.Cores.IsNull() {
		t.Errorf("unknown cores not nulled: %v", m.Cores)
	}
}

// TestServerStatusString pins the vocabulary boundary that caused a real bug:
// /deploy/status says "ready", /servers says "running", and Create once
// recorded the former while Read recorded the latter, so every plan after an
// apply showed drift. The status attribute is derived from the durable
// /servers record only.
func TestServerStatusString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		srv  *gigahost.Server
		want string
	}{
		// Precedence matters: installing masks both rescue and running.
		{"installing wins over running", &gigahost.Server{StatusInstall: true, Status: true}, "installing"},
		{"installing wins over rescue", &gigahost.Server{StatusInstall: true, StatusRescue: true}, "installing"},
		{"rescue wins over running", &gigahost.Server{StatusRescue: true, Status: true}, "rescue"},
		{"running", &gigahost.Server{Status: true}, "running"},
		{"off", &gigahost.Server{}, "off"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := serverStatusString(tc.srv); got != tc.want {
				t.Errorf("serverStatusString() = %q, want %q", got, tc.want)
			}
		})
	}

	// The two vocabularies overlap on installing and rescue but must never be
	// assigned across: a deploy-side word in state is the original bug.
	deployOnly := map[string]bool{
		string(gigahost.DeployStatusReady):     true,
		string(gigahost.DeployStatusWaiting):   true,
		string(gigahost.DeployStatusDeploying): true,
		string(gigahost.DeployStatusISO):       true,
	}

	for _, tc := range tests {
		if deployOnly[tc.want] {
			t.Errorf("serverStatusString returned %q, a /deploy/status word; the "+
				"status attribute must use the /servers vocabulary", tc.want)
		}
	}
}

// TestProvisionStatusesAreExhaustive guards the classifier: statusIsFailure
// treats anything it does not recognise as a failure, so a status added
// upstream would silently abort a healthy deploy. Every value the client
// knows must be classified as either provisioning or terminal-ready.
func TestProvisionStatusesAreExhaustive(t *testing.T) {
	t.Parallel()

	all := []gigahost.DeployProvisionStatus{
		gigahost.DeployStatusWaiting,
		gigahost.DeployStatusDeploying,
		gigahost.DeployStatusInstalling,
		gigahost.DeployStatusReady,
		gigahost.DeployStatusRescue,
		gigahost.DeployStatusISO,
	}

	for _, st := range all {
		if !knownProvisioningStatuses[st] && !terminalReadyStatuses[st] {
			t.Errorf("%q is in the client's status enum but classified as neither "+
				"provisioning nor terminal-ready, so statusIsFailure would abort on it", st)
		}
	}
}
