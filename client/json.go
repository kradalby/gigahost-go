package client

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The Gigahost API is a wire format that predates RFC-strict JSON habits:
// booleans arrive as "0"/"1" strings, unix timestamps as either numbers or
// numeric strings, and date-times as "2025-12-31 23:59:59". The helpers
// here normalise those representations into idiomatic Go values so
// downstream types can use real bool / int64 / time.Time fields.
//
// Each helper intentionally accepts the superset of shapes we have seen in
// the API responses rather than a single strict shape.  When marshalling
// back out for POST/PUT requests we always emit the API's expected shape.

// apiBool is a bool that can unmarshal from any of the forms the Gigahost
// API uses: "0"/"1" (quoted), true/false, 0/1.  It marshals as "1" or "0"
// to match API request expectations.
type apiBool bool

// UnmarshalJSON accepts "0"/"1" (string), true/false, or 0/1 (number).
func (b *apiBool) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*b = false

		return nil
	}

	// Strip surrounding quotes for the string-form representations.
	if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	switch strings.ToLower(string(trimmed)) {
	case "1", "true", "yes", "on":
		*b = true
	case "0", "false", "no", "off", "":
		*b = false
	default:
		return fmt.Errorf("gigahost: cannot decode %q as bool", string(data))
	}

	return nil
}

// MarshalJSON emits the API's preferred shape (`"1"` or `"0"`).
func (b apiBool) MarshalJSON() ([]byte, error) {
	if b {
		return []byte(`"1"`), nil
	}

	return []byte(`"0"`), nil
}

// apiInt is an int64 that accepts either a JSON number or a JSON string
// containing a base-10 integer.
type apiInt int64

// UnmarshalJSON accepts `42` or `"42"`.
func (i *apiInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*i = 0

		return nil
	}

	if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	if len(trimmed) == 0 {
		*i = 0

		return nil
	}

	n, err := strconv.ParseInt(string(trimmed), 10, 64)
	if err != nil {
		return fmt.Errorf("gigahost: cannot decode %q as int: %w", string(data), err)
	}

	*i = apiInt(n)

	return nil
}

// MarshalJSON emits a JSON number.
func (i apiInt) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(i), 10)), nil
}

// apiString is a string that accepts either a JSON string or a JSON number.
// Several ID-like fields (group_id, product_id, price_id, region_ids) arrive
// unquoted as numbers in some responses but quoted in others; this normalises
// both to a Go string. It marshals back out as a quoted string.
type apiString string

// UnmarshalJSON accepts "42" or 42 (null becomes "").
func (s *apiString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*s = ""

		return nil
	}

	if trimmed[0] == '"' {
		var str string
		if err := json.Unmarshal(trimmed, &str); err != nil {
			return fmt.Errorf("gigahost: cannot decode %q as string: %w", string(data), err)
		}

		*s = apiString(str)

		return nil
	}

	// Unquoted: only a number is meaningful here. Accepting anything else
	// would store the literal text of a bool, array or object and report
	// success, turning a shape change upstream into silent corruption.
	if c := trimmed[0]; c != '-' && (c < '0' || c > '9') {
		return fmt.Errorf("gigahost: cannot decode %q as string: want a string or a number", string(data))
	}

	*s = apiString(trimmed)

	return nil
}

// MarshalJSON emits a quoted JSON string.
func (s apiString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// apiStrings converts a slice of apiString to a plain []string, preserving nil.
func apiStrings(in []apiString) []string {
	if in == nil {
		return nil
	}

	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}

	return out
}

// apiUnixTime decodes a unix timestamp (seconds) which the API returns
// either as a JSON number or as a quoted string.
type apiUnixTime time.Time

// UnmarshalJSON accepts `1700000000` or `"1700000000"`.
func (t *apiUnixTime) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*t = apiUnixTime(time.Time{})

		return nil
	}

	if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("0")) {
		*t = apiUnixTime(time.Time{})

		return nil
	}

	secs, err := strconv.ParseInt(string(trimmed), 10, 64)
	if err != nil {
		return fmt.Errorf("gigahost: cannot decode %q as unix time: %w", string(data), err)
	}

	*t = apiUnixTime(time.Unix(secs, 0).UTC())

	return nil
}

// Time returns the embedded [time.Time].
func (t *apiUnixTime) Time() time.Time { return time.Time(*t) }

// apiDateTime decodes the "2006-01-02 15:04:05" format commonly used by
// the Gigahost API for things like domain expiry dates. Returned times are
// interpreted as UTC; the API does not document a timezone.
type apiDateTime time.Time

const apiDateTimeLayout = "2006-01-02 15:04:05"

// UnmarshalJSON accepts quoted "YYYY-MM-DD HH:MM:SS" strings.
func (t *apiDateTime) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*t = apiDateTime(time.Time{})

		return nil
	}

	if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	s := string(trimmed)
	if s == "" {
		*t = apiDateTime(time.Time{})

		return nil
	}

	parsed, err := time.ParseInLocation(apiDateTimeLayout, s, time.UTC)
	if err != nil {
		// Some endpoints return RFC3339 even for these fields; try that
		// as a secondary format before giving up.
		if p2, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			*t = apiDateTime(p2.UTC())

			return nil
		}

		return fmt.Errorf("gigahost: cannot decode %q as datetime: %w", s, err)
	}

	*t = apiDateTime(parsed)

	return nil
}

// Time returns the embedded [time.Time].
func (t *apiDateTime) Time() time.Time { return time.Time(*t) }

// marshalJSON is a small shim so service code doesn't need to import
// [encoding/json/v2] directly.
func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func unmarshalJSON(data []byte, v any) error { return json.Unmarshal(data, v) }
