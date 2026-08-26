package cli

import (
	"slices"
	"testing"
)

// TestHoistFlags pins the behaviour every other CLI has and this one did not:
// a flag written after the positional argument still takes effect.
func TestHoistFlags(t *testing.T) {
	t.Parallel()

	root := NewCommand(Options{})

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			// The case that started this: --type was silently dropped, and the
			// error never mentioned it.
			name: "flag after positional",
			args: []string{"dns", "zones", "create", "example.no", "--type", "NATIVE"},
			want: []string{"dns", "zones", "create", "--type", "NATIVE", "example.no"},
		},
		{
			name: "already in flags-first order is untouched",
			args: []string{"dns", "zones", "create", "--type", "NATIVE", "example.no"},
			want: []string{"dns", "zones", "create", "--type", "NATIVE", "example.no"},
		},
		{
			// Silent before: -o and json were sent to the API as order IDs.
			name: "global flag after a variadic positional",
			args: []string{"deploy", "status", "123", "-o", "json"},
			want: []string{"deploy", "status", "-o", "json", "123"},
		},
		{
			name: "boolean flag does not swallow the next argument",
			args: []string{"dns", "zones", "create", "example.no", "--defaults"},
			want: []string{"dns", "zones", "create", "--defaults", "example.no"},
		},
		{
			name: "flag=value form carries its own value",
			args: []string{"dns", "zones", "create", "example.no", "--type=MASTER"},
			want: []string{"dns", "zones", "create", "--type=MASTER", "example.no"},
		},
		{
			name: "several flags keep their order",
			args: []string{"dns", "records", "delete", "r1", "--zone", "z", "--type", "A", "--value", "1.2.3.4"},
			want: []string{"dns", "records", "delete", "--zone", "z", "--type", "A", "--value", "1.2.3.4", "r1"},
		},
		{
			name: "double dash ends flag processing",
			args: []string{"dns", "zones", "create", "--", "--weird-zone-name"},
			want: []string{"dns", "zones", "create", "--", "--weird-zone-name"},
		},
		{
			name: "subcommand names are never hoisted past",
			args: []string{"servers", "get", "web01"},
			want: []string{"servers", "get", "web01"},
		},
		{
			// Reordering here would turn "unknown subcommand" into "unknown
			// flag", which is a worse error for the same mistake.
			name: "unknown command is left exactly as written",
			args: []string{"bogus", "thing", "--flag"},
			want: []string{"bogus", "thing", "--flag"},
		},
		{
			name: "empty",
			args: []string{},
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := hoistFlags(root, tc.args)
			if !slices.Equal(got, tc.want) {
				t.Errorf("hoistFlags(%q)\n got %q\nwant %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestHoistFlagsPreservesEveryArgument is the safety property: reordering must
// never drop or invent an argument, whatever the input.
func TestHoistFlagsPreservesEveryArgument(t *testing.T) {
	t.Parallel()

	root := NewCommand(Options{})

	for _, args := range [][]string{
		{"dns", "records", "create", "-z", "5000", "www", "--type", "A"},
		{"servers", "reverse", "18394", "--ip-id", "9", "--dns", "a.example.no"},
		{"account", "api-keys", "update", "3", "--label", "ci"},
		{"-o", "json", "servers", "list"},
	} {
		got := hoistFlags(root, args)

		if len(got) != len(args) {
			t.Errorf("hoistFlags(%q) changed the argument count: %q", args, got)

			continue
		}

		sortedGot, sortedWant := slices.Clone(got), slices.Clone(args)
		slices.Sort(sortedGot)
		slices.Sort(sortedWant)

		if !slices.Equal(sortedGot, sortedWant) {
			t.Errorf("hoistFlags(%q) changed the arguments: %q", args, got)
		}
	}
}
