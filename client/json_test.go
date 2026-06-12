package client

import (
	"testing"
	"time"
)

func TestAPIBoolUnmarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want bool
		err  bool
	}{
		{"quoted one", `"1"`, true, false},
		{"quoted zero", `"0"`, false, false},
		{"quoted true", `"true"`, true, false},
		{"quoted false", `"false"`, false, false},
		{"unquoted true", `true`, true, false},
		{"unquoted false", `false`, false, false},
		{"unquoted one", `1`, true, false},
		{"unquoted zero", `0`, false, false},
		{"null", `null`, false, false},
		{"empty string", `""`, false, false},
		{"invalid", `"maybe"`, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got apiBool

			err := got.UnmarshalJSON([]byte(tc.in))
			if tc.err {
				if err == nil {
					t.Fatalf("want error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if bool(got) != tc.want {
				t.Errorf("got %v, want %v", bool(got), tc.want)
			}
		})
	}
}

func TestAPIBoolMarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   apiBool
		want string
	}{
		{true, `"1"`},
		{false, `"0"`},
	}

	for _, tc := range cases {
		got, err := tc.in.MarshalJSON()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if string(got) != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
	}
}

func TestAPIIntUnmarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want int64
		err  bool
	}{
		{"quoted", `"1234"`, 1234, false},
		{"unquoted", `1234`, 1234, false},
		{"negative", `"-7"`, -7, false},
		{"null", `null`, 0, false},
		{"empty", `""`, 0, false},
		{"invalid", `"abc"`, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got apiInt

			err := got.UnmarshalJSON([]byte(tc.in))
			if tc.err {
				if err == nil {
					t.Fatalf("want error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if int64(got) != tc.want {
				t.Errorf("got %d, want %d", int64(got), tc.want)
			}
		})
	}
}

func TestAPIUnixTimeUnmarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want time.Time
		err  bool
	}{
		{"quoted", `"1700000000"`, time.Unix(1700000000, 0).UTC(), false},
		{"unquoted", `1700000000`, time.Unix(1700000000, 0).UTC(), false},
		{"zero", `"0"`, time.Time{}, false},
		{"null", `null`, time.Time{}, false},
		{"empty", `""`, time.Time{}, false},
		{"invalid", `"not-a-time"`, time.Time{}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got apiUnixTime

			err := got.UnmarshalJSON([]byte(tc.in))
			if tc.err {
				if err == nil {
					t.Fatalf("want error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !time.Time(got).Equal(tc.want) {
				t.Errorf("got %v, want %v", time.Time(got), tc.want)
			}
		})
	}
}

func TestAPIDateTimeUnmarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want time.Time
		err  bool
	}{
		{"standard", `"2025-12-31 23:59:59"`, time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC), false},
		{"rfc3339", `"2025-12-31T23:59:59Z"`, time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC), false},
		{"null", `null`, time.Time{}, false},
		{"empty", `""`, time.Time{}, false},
		{"invalid", `"hello"`, time.Time{}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got apiDateTime

			err := got.UnmarshalJSON([]byte(tc.in))
			if tc.err {
				if err == nil {
					t.Fatalf("want error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !time.Time(got).Equal(tc.want) {
				t.Errorf("got %v, want %v", time.Time(got), tc.want)
			}
		})
	}
}
