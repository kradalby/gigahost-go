package tfprovider

import (
	"strings"
	"testing"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// ---------------------------------------------------------------------------
// Task 1: parseImportID
// ---------------------------------------------------------------------------

func TestParseImportID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id      string
		wantErr bool
		want    []string
	}{
		{id: "z1/r2", want: []string{"z1", "r2"}},
		{id: "example.no/r2", want: []string{"example.no", "r2"}},
		{id: "z1", wantErr: true},
		{id: "/r2", wantErr: true},
		{id: "z1/", wantErr: true},
		{id: "a/b/c", wantErr: true},
	}

	// Non-2-arity: a single-name format must reject composite IDs.
	if _, err := parseImportID("123/456", "asn"); err == nil || !strings.Contains(err.Error(), "<asn>") {
		t.Errorf("parseImportID(123/456, asn) = %v, want error mentioning <asn>", err)
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()

			got, err := parseImportID(tt.id, "zone", "record_id")

			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseImportID(%q) = %v, want error", tt.id, got)
				}

				if !strings.Contains(err.Error(), "<zone>/<record_id>") {
					t.Errorf("error %q does not mention expected format <zone>/<record_id>", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseImportID(%q) unexpected error: %v", tt.id, err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("parseImportID(%q) = %v, want %v", tt.id, got, tt.want)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parts[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 1: normalizeASNImportID
// ---------------------------------------------------------------------------

func TestNormalizeASNImportID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "212345", want: "212345"},
		{in: "AS212345", want: "212345"},
		{in: "as212345", want: "212345"},
		{in: " AS212345 ", want: "212345"},
		{in: "", wantErr: true},
		{in: "ASabc", wantErr: true},
		{in: "AS", wantErr: true},
		{in: "AS123abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeASNImportID(tt.in)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeASNImportID(%q) = %q, want error", tt.in, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("normalizeASNImportID(%q) unexpected error: %v", tt.in, err)
			}

			if got != tt.want {
				t.Errorf("normalizeASNImportID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 2: findZoneID
// ---------------------------------------------------------------------------

func TestFindZoneID(t *testing.T) {
	t.Parallel()

	zones := []gigahost.Zone{
		{ID: "123", Name: "example.no"},
		{ID: "456", Name: "other.no"},
	}

	tests := []struct {
		identifier string
		want       string
		wantErr    bool
	}{
		{identifier: "123", want: "123"},
		{identifier: "example.no", want: "123"},
		{identifier: "EXAMPLE.no", want: "123"},
		{identifier: "missing.no", wantErr: true},
		{identifier: "999", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			t.Parallel()

			got, err := findZoneID(zones, tt.identifier)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("findZoneID(%q) = %q, want error", tt.identifier, got)
				}

				if !strings.Contains(err.Error(), tt.identifier) {
					t.Errorf("error %q does not mention identifier %q", err, tt.identifier)
				}

				return
			}

			if err != nil {
				t.Fatalf("findZoneID(%q) unexpected error: %v", tt.identifier, err)
			}

			if got != tt.want {
				t.Errorf("findZoneID(%q) = %q, want %q", tt.identifier, got, tt.want)
			}
		})
	}
}
