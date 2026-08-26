package cli

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWriteAllTokensIsByteStable pins the reason writeAllTokens marshals with
// json.Deterministic: encoding/json/v2 does not sort map keys on its own, so
// without the option every `auth login` rewrote token.json with the accounts
// in a fresh random order. The file lives in a config directory people back
// up and diff, and churn there is indistinguishable from a real change.
func TestWriteAllTokensIsByteStable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "token.json")

	tokens := map[string]string{
		"anna@example.no":   "tok-a",
		"bjorn@example.no":  "tok-b",
		"carl@example.no":   "tok-c",
		"dina@example.no":   "tok-d",
		"erik@example.no":   "tok-e",
		"frida@example.no":  "tok-f",
		"gunnar@example.no": "tok-g",
		"hilde@example.no":  "tok-h",
	}

	var first []byte

	for i := range 50 {
		if err := writeAllTokens(path, tokens); err != nil {
			t.Fatalf("writeAllTokens: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read token file: %v", err)
		}

		if first == nil {
			first = got

			continue
		}

		if !bytes.Equal(first, got) {
			t.Fatalf("write %d produced different bytes:\nfirst: %s\n got: %s", i, first, got)
		}
	}

	// Deterministic means sorted by key, which is also the order a human
	// reading the file expects.
	offsets := make([]int, 0, len(tokens))
	for _, user := range slices.Sorted(maps.Keys(tokens)) {
		offsets = append(offsets, strings.Index(string(first), user))
	}

	if !slices.IsSorted(offsets) {
		t.Errorf("accounts are not in sorted order: %s", first)
	}

	back, err := readTokenFile(path)
	if err != nil {
		t.Fatalf("readTokenFile: %v", err)
	}

	if !maps.Equal(back, tokens) {
		t.Errorf("round trip lost data: got %v, want %v", back, tokens)
	}
}
