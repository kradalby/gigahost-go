package client

import (
	"strings"
	"testing"
)

// TestRedactSecrets is the guard on the debug logger's promise. It logs the
// authenticate exchange, a deploy response and an API-key creation verbatim,
// each of which carries a credential that must never reach a log file.
func TestRedactSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body, mustNotContain string
	}{
		{"authenticate request", `{"username":"a@b.no","password":"hunter2","code":"123456"}`, "hunter2"},
		{"authenticate response", `{"token":"eyJhbGciOi.secret.value"}`, "eyJhbGciOi.secret.value"},
		{"deploy root password", `{"server_id":"1","root_passwd":"Tr0ub4dor"}`, "Tr0ub4dor"},
		{"api key secret", `{"key_id":"3","secret":"gh_live_abcdef"}`, "gh_live_abcdef"},
		{"ipmi password", `{"kvm_host":"h","kvm_password":"kvmpass"}`, "kvmpass"},
		{"spaced json", `{"password" : "spaced-secret"}`, "spaced-secret"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := truncateBody([]byte(tc.body))

			if strings.Contains(got, tc.mustNotContain) {
				t.Errorf("secret survived redaction: %s", got)
			}

			if !strings.Contains(got, "[redacted]") {
				t.Errorf("nothing was redacted in %s", got)
			}
		})
	}
}

// TestRedactSecretsKeepsContext checks redaction stays surgical: the point of
// a debug log is to show what went on the wire.
func TestRedactSecretsKeepsContext(t *testing.T) {
	t.Parallel()

	got := truncateBody([]byte(`{"username":"a@b.no","password":"hunter2"}`))

	if !strings.Contains(got, "a@b.no") {
		t.Errorf("redaction removed non-secret context: %s", got)
	}
}

// TestRedactBeforeTruncate pins the ordering: a secret past the 2KiB cut must
// still be redacted, not merely hidden by truncation of an earlier field.
func TestRedactBeforeTruncate(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat("x", debugBodyLimit)
	got := truncateBody([]byte(`{"pad":"` + padding + `","password":"tail-secret"}`))

	if strings.Contains(got, "tail-secret") {
		t.Errorf("a secret beyond the truncation point was not redacted: %s", got[len(got)-80:])
	}
}
