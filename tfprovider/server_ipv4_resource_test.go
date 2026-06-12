package tfprovider

import (
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func TestNewestIP(t *testing.T) {
	before := map[string]bool{"1": true, "2": true}

	tests := []struct {
		name   string
		after  []gigahost.ServerIP
		wantID string
	}{
		{
			name:   "no new ip",
			after:  []gigahost.ServerIP{{ID: "1", Address: "192.0.2.1"}, {ID: "2", Address: "192.0.2.2"}},
			wantID: "",
		},
		{
			name:   "new ipv4 preferred over new ipv6",
			after:  []gigahost.ServerIP{{ID: "1"}, {ID: "9", Address: "2001:db8::9"}, {ID: "10", Address: "192.0.2.10"}},
			wantID: "10",
		},
		{
			name:   "new ipv6 only as fallback",
			after:  []gigahost.ServerIP{{ID: "1"}, {ID: "9", Address: "2001:db8::9"}},
			wantID: "9",
		},
		{
			name:   "blank id ignored",
			after:  []gigahost.ServerIP{{ID: "", Address: "192.0.2.5"}, {ID: "10", Address: "192.0.2.10"}},
			wantID: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newestIP(before, tt.after)
			if tt.wantID == "" {
				if got != nil {
					t.Fatalf("newestIP = %+v, want nil", got)
				}

				return
			}

			if got == nil || got.ID != tt.wantID {
				t.Fatalf("newestIP = %+v, want id %s", got, tt.wantID)
			}
		})
	}
}

func TestFindIPByID(t *testing.T) {
	ips := []gigahost.ServerIP{{ID: "1", Address: "a"}, {ID: "2", Address: "b"}}

	if got := findIPByID(ips, "2"); got == nil || got.Address != "b" {
		t.Fatalf("findIPByID(2) = %+v", got)
	}

	if got := findIPByID(ips, "9"); got != nil {
		t.Fatalf("findIPByID(9) = %+v, want nil", got)
	}
}

func TestApplyIPToModel(t *testing.T) {
	var m serverIPv4Model
	applyIPToModel(&m, &gigahost.ServerIP{
		ID: "10", Address: "192.0.2.10", Gateway: "192.0.2.1", Netmask: "255.255.255.0", Version: "4",
	})

	if m.ID.ValueString() != "10" || m.Address.ValueString() != "192.0.2.10" ||
		m.Gateway.ValueString() != "192.0.2.1" || m.Netmask.ValueString() != "255.255.255.0" ||
		m.Version.ValueString() != "4" {
		t.Fatalf("applyIPToModel = %+v", m)
	}

	// Empty fields collapse to null, not "".
	var empty serverIPv4Model
	applyIPToModel(&empty, &gigahost.ServerIP{ID: "11"})

	if !empty.Address.IsNull() {
		t.Fatalf("empty address should be null, got %v", empty.Address)
	}
}
