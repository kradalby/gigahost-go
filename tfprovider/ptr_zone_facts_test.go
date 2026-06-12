package tfprovider

import (
	"testing"
)

func TestPtrZoneFacts(t *testing.T) {
	tests := []struct {
		name     string
		arpaName string
		wantVer  string
		wantPfx  string
		wantErr  bool
	}{
		// IPv4 /24 — 3 octets reversed
		{
			name:     "ipv4 /24",
			arpaName: "168.125.185.in-addr.arpa",
			wantVer:  "ipv4",
			wantPfx:  "185.125.168.0/24",
		},
		// IPv4 /16 — 2 octets reversed
		{
			name:     "ipv4 /16",
			arpaName: "125.185.in-addr.arpa",
			wantVer:  "ipv4",
			wantPfx:  "185.125.0.0/16",
		},
		// IPv4 /8 — 1 octet reversed
		{
			name:     "ipv4 /8",
			arpaName: "185.in-addr.arpa",
			wantVer:  "ipv4",
			wantPfx:  "185.0.0.0/8",
		},
		// IPv6 /32 — 8 nibbles reversed (2 full 16-bit groups = 32 bits)
		{
			name:     "ipv6 /32",
			arpaName: "0.e.4.9.3.0.a.2.ip6.arpa",
			wantVer:  "ipv6",
			wantPfx:  "2a03:94e0::/32",
		},
		// IPv6 /16 — 4 nibbles reversed (1 full 16-bit group = 16 bits)
		{
			name:     "ipv6 /16",
			arpaName: "3.0.a.2.ip6.arpa",
			wantVer:  "ipv6",
			wantPfx:  "2a03::/16",
		},
		// Malformed: no suffix
		{
			name:     "no arpa suffix",
			arpaName: "example.com",
			wantErr:  true,
		},
		// Malformed: empty
		{
			name:     "empty",
			arpaName: "",
			wantErr:  true,
		},
		// Malformed: in-addr.arpa with no octets
		{
			name:     "bare in-addr.arpa",
			arpaName: "in-addr.arpa",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ver, pfx, err := ptrZoneFacts(tc.arpaName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ptrZoneFacts(%q): want error, got nil", tc.arpaName)
				}

				return
			}

			if err != nil {
				t.Fatalf("ptrZoneFacts(%q): unexpected error: %v", tc.arpaName, err)
			}

			if ver != tc.wantVer {
				t.Errorf("ptrZoneFacts(%q): ip_version = %q, want %q", tc.arpaName, ver, tc.wantVer)
			}

			if pfx != tc.wantPfx {
				t.Errorf("ptrZoneFacts(%q): prefix = %q, want %q", tc.arpaName, pfx, tc.wantPfx)
			}
		})
	}
}
