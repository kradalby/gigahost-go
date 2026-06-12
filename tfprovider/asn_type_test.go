package tfprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAsnValue_StringSemanticEquals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	cases := []struct {
		name      string
		receiver  string
		other     string
		wantEqual bool
	}{
		{
			name:      "equal bare numeric",
			receiver:  "212345",
			other:     "212345",
			wantEqual: true,
		},
		{
			name:      "AS-prefix vs bare",
			receiver:  "AS212345",
			other:     "212345",
			wantEqual: true,
		},
		{
			name:      "lowercase as-prefix vs bare",
			receiver:  "as212345",
			other:     "212345",
			wantEqual: true,
		},
		{
			name:      "bare vs AS-prefix",
			receiver:  "212345",
			other:     "AS212345",
			wantEqual: true,
		},
		{
			name:      "AS-prefix with leading whitespace",
			receiver:  " AS212345",
			other:     "212345",
			wantEqual: true,
		},
		{
			name:      "bare with surrounding whitespace",
			receiver:  "  212345  ",
			other:     "212345",
			wantEqual: true,
		},
		{
			name:      "different ASNs bare",
			receiver:  "212345",
			other:     "65000",
			wantEqual: false,
		},
		{
			name:      "different ASNs AS-prefix vs AS-prefix",
			receiver:  "AS212345",
			other:     "AS65000",
			wantEqual: false,
		},
		{
			name:      "garbage vs number — not equal, falls back to literal",
			receiver:  "notanasn",
			other:     "212345",
			wantEqual: false,
		},
		{
			name:      "both garbage — literal equal",
			receiver:  "notanasn",
			other:     "notanasn",
			wantEqual: true,
		},
		{
			name:      "AS-only no digits",
			receiver:  "AS",
			other:     "212345",
			wantEqual: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recv := asnValue{StringValue: types.StringValue(tc.receiver)}
			other := asnValue{StringValue: types.StringValue(tc.other)}

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
