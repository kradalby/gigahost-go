package tfprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSSHPublicKeyValue_StringSemanticEquals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	keyA := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBase64payload user@host"
	keyB := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDdifferentkey user@host"

	cases := []struct {
		name      string
		receiver  string
		other     string
		wantEqual bool
	}{
		{
			name:      "identical keys",
			receiver:  keyA,
			other:     keyA,
			wantEqual: true,
		},
		{
			name:      "trailing newline on other",
			receiver:  keyA,
			other:     keyA + "\n",
			wantEqual: true,
		},
		{
			name:      "trailing newline on receiver",
			receiver:  keyA + "\n",
			other:     keyA,
			wantEqual: true,
		},
		{
			name:      "both have surrounding whitespace",
			receiver:  "  " + keyA + "\n",
			other:     keyA + "  ",
			wantEqual: true,
		},
		{
			name:      "different keys",
			receiver:  keyA,
			other:     keyB,
			wantEqual: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recv := sshPublicKeyValue{StringValue: types.StringValue(tc.receiver)}
			other := sshPublicKeyValue{StringValue: types.StringValue(tc.other)}

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
