package client

import (
	"encoding/base64"
	"fmt"
)

// Meta is the metadata returned on every Gigahost API response.
//
// The API always returns a `meta` object and, for successful data
// responses, a `data` field. A few endpoints (most notably
// /my/account, /my/invoices, /servers/{id}/powerstate and
// /servers/{id}/reinstall) put the actual payload at the top level
// rather than inside `data`; the client code handles those as
// special cases.
type Meta struct {
	Status        int    `json:"status"`
	StatusMessage string `json:"status_message"`
	Message       string `json:"message,omitempty"`
}

// envelope is the generic wrapper used for standard responses.
// The Data field is kept as raw bytes so each service can decode it
// into the concrete response type it needs, including list vs
// single-object responses that share a URL shape.
type envelope struct {
	Success bool         `json:"success,omitempty"`
	Meta    Meta         `json:"meta"`
	Data    rawJSONValue `json:"data,omitempty"`
}

// rawJSONValue is a []byte that round-trips raw JSON, similar in spirit
// to json.RawMessage. We define our own type because we are on the v2
// package and want the definition local to this module.
type rawJSONValue []byte

// MarshalJSON returns the bytes as-is, or `null` if empty.
func (r rawJSONValue) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}

	out := make([]byte, len(r))
	copy(out, r)

	return out, nil
}

// UnmarshalJSON captures the raw bytes for later decoding.
func (r *rawJSONValue) UnmarshalJSON(data []byte) error {
	*r = make([]byte, len(data))
	copy(*r, data)

	return nil
}

// Base64Bytes decodes a base64-encoded binary payload (for example,
// the PNG graphs returned by /servers/{id}/port_bits).
func Base64Bytes(s string) ([]byte, error) {
	// The API sometimes prefixes the data URL scheme, sometimes not.
	for _, prefix := range []string{"data:image/png;base64,", "data:image/gif;base64,"} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]

			break
		}
	}

	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("gigahost: base64 decode: %w", err)
	}

	return b, nil
}
