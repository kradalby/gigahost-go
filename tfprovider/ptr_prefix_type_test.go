package tfprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestCanonicalizePTRPrefix verifies the pure canonicalization function
// against the forms accepted by the schema and the CIDR output of ptrZoneFacts.
func TestCanonicalizePTRPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		// IPv4 bare-octet forms.
		{
			name:  "ipv4 bare /24",
			input: "185.181.63",
			want:  "185.181.63.0/24",
		},
		{
			name:  "ipv4 bare /16",
			input: "185.181",
			want:  "185.181.0.0/16",
		},
		{
			name:  "ipv4 bare /8",
			input: "185",
			want:  "185.0.0.0/8",
		},
		// IPv4 CIDR pass-through.
		{
			name:  "ipv4 cidr /24",
			input: "185.181.63.0/24",
			want:  "185.181.63.0/24",
		},
		{
			name:  "ipv4 cidr /16",
			input: "185.181.0.0/16",
			want:  "185.181.0.0/16",
		},
		// IPv4 CIDR with host bits — masked to network address.
		{
			name:  "ipv4 cidr with host bits masked",
			input: "185.181.63.5/24",
			want:  "185.181.63.0/24",
		},
		// IPv6 bare prefix forms (no length).
		{
			name:  "ipv6 bare /32",
			input: "2a03:94e0::",
			want:  "2a03:94e0::/32",
		},
		{
			name:  "ipv6 bare /16",
			input: "2a03::",
			want:  "2a03::/16",
		},
		// IPv6 CIDR pass-through.
		{
			name:  "ipv6 cidr /32",
			input: "2a03:94e0::/32",
			want:  "2a03:94e0::/32",
		},
		{
			name:  "ipv6 cidr /16",
			input: "2a03::/16",
			want:  "2a03::/16",
		},
		// Leading/trailing whitespace is stripped.
		{
			name:  "ipv4 with whitespace",
			input: "  185.181.63  ",
			want:  "185.181.63.0/24",
		},
		// Error cases.
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid ipv4 cidr",
			input:   "999.999.999.999/24",
			wantErr: true,
		},
		{
			name:    "garbage",
			input:   "not-a-prefix",
			wantErr: true,
		},
		{
			name:    "ipv4 too many octets for bare form",
			input:   "185.181.63.0.1",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := canonicalizePTRPrefix(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("canonicalizePTRPrefix(%q): want error, got %q", tc.input, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("canonicalizePTRPrefix(%q): unexpected error: %v", tc.input, err)
			}

			if got != tc.want {
				t.Errorf("canonicalizePTRPrefix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestCanonicalizePTRPrefix_AgainstPtrZoneFacts cross-checks that the
// canonicalizer agrees with ptrZoneFacts for the same zones used in
// ptr_zone_facts_test.go.
func TestCanonicalizePTRPrefix_AgainstPtrZoneFacts(t *testing.T) {
	t.Parallel()

	// Each entry: the bare/CIDR form a user might write → the CIDR that
	// ptrZoneFacts returns for the corresponding arpa zone name.
	cases := []struct {
		configForm string
		canonWant  string // == ptrZoneFacts output
	}{
		{configForm: "185.125.168", canonWant: "185.125.168.0/24"}, // /24
		{configForm: "185.125.168.0/24", canonWant: "185.125.168.0/24"},
		{configForm: "185.125", canonWant: "185.125.0.0/16"},     // /16
		{configForm: "185", canonWant: "185.0.0.0/8"},            // /8
		{configForm: "2a03:94e0::", canonWant: "2a03:94e0::/32"}, // /32
		{configForm: "2a03:94e0::/32", canonWant: "2a03:94e0::/32"},
		{configForm: "2a03::", canonWant: "2a03::/16"}, // /16
	}

	for _, tc := range cases {
		t.Run(tc.configForm, func(t *testing.T) {
			t.Parallel()

			got, err := canonicalizePTRPrefix(tc.configForm)
			if err != nil {
				t.Fatalf("canonicalizePTRPrefix(%q): %v", tc.configForm, err)
			}

			if got != tc.canonWant {
				t.Errorf("canonicalizePTRPrefix(%q) = %q, want %q (ptrZoneFacts output)",
					tc.configForm, got, tc.canonWant)
			}
		})
	}
}

func TestPtrPrefixValue_StringSemanticEquals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	cases := []struct {
		name      string
		receiver  string
		other     string
		wantEqual bool
	}{
		{
			name:      "ipv4 bare vs cidr equal",
			receiver:  "185.181.63",
			other:     "185.181.63.0/24",
			wantEqual: true,
		},
		{
			name:      "ipv4 cidr vs bare equal",
			receiver:  "185.181.63.0/24",
			other:     "185.181.63",
			wantEqual: true,
		},
		{
			name:      "ipv4 identical cidr",
			receiver:  "185.181.63.0/24",
			other:     "185.181.63.0/24",
			wantEqual: true,
		},
		{
			name:      "ipv4 different prefixes",
			receiver:  "185.181.63",
			other:     "185.181.64.0/24",
			wantEqual: false,
		},
		{
			name:      "ipv4 different prefix lengths",
			receiver:  "185.181",
			other:     "185.181.63.0/24",
			wantEqual: false,
		},
		{
			name:      "ipv6 bare vs cidr equal",
			receiver:  "2a03:94e0::",
			other:     "2a03:94e0::/32",
			wantEqual: true,
		},
		{
			name:      "ipv6 cidr vs bare equal",
			receiver:  "2a03:94e0::/32",
			other:     "2a03:94e0::",
			wantEqual: true,
		},
		{
			name:      "ipv6 identical cidr",
			receiver:  "2a03:94e0::/32",
			other:     "2a03:94e0::/32",
			wantEqual: true,
		},
		{
			name:      "ipv6 different prefixes",
			receiver:  "2a03:94e0::",
			other:     "2a03:94e1::/32",
			wantEqual: false,
		},
		{
			name:      "garbage vs valid — not equal, falls back to literal",
			receiver:  "not-a-prefix",
			other:     "185.181.63.0/24",
			wantEqual: false,
		},
		{
			name:      "both garbage — literal equal",
			receiver:  "not-a-prefix",
			other:     "not-a-prefix",
			wantEqual: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recv := ptrPrefixValue{StringValue: types.StringValue(tc.receiver)}
			other := ptrPrefixValue{StringValue: types.StringValue(tc.other)}

			got, diags := recv.StringSemanticEquals(ctx, other)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}

			if got != tc.wantEqual {
				t.Errorf("StringSemanticEquals(%q, %q) = %v, want %v",
					tc.receiver, tc.other, got, tc.wantEqual)
			}
		})
	}
}
