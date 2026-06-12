package tfprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ basetypes.StringTypable                    = ptrPrefixType{}
	_ basetypes.StringValuableWithSemanticEquals = ptrPrefixValue{}
)

// ptrPrefixType is a custom string type for PTR zone IP prefixes. It produces
// ptrPrefixValue values that implement semantic equality so that bare-form
// inputs ("185.181.63", "2a03:94e0::") compare equal to the canonical CIDR
// forms stored by Read ("185.181.63.0/24", "2a03:94e0::/32").
type ptrPrefixType struct {
	basetypes.StringType
}

func (t ptrPrefixType) Equal(o attr.Type) bool {
	_, ok := o.(ptrPrefixType)

	return ok
}

func (t ptrPrefixType) String() string {
	return "ptrPrefixType"
}

func (t ptrPrefixType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return ptrPrefixValue{StringValue: in}, nil
}

func (t ptrPrefixType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	attrValue, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}

	sv, ok := attrValue.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", attrValue)
	}

	return ptrPrefixValue{StringValue: sv}, nil
}

func (t ptrPrefixType) ValueType(_ context.Context) attr.Value {
	return ptrPrefixValue{}
}

// ptrPrefixValue is the value type for ptrPrefixType. Two prefix values are
// semantically equal when they canonicalise to the same CIDR string. If
// either value fails to canonicalise, equality falls back to a literal
// string comparison.
type ptrPrefixValue struct {
	basetypes.StringValue
}

func (v ptrPrefixValue) Type(_ context.Context) attr.Type {
	return ptrPrefixType{}
}

func (v ptrPrefixValue) Equal(o attr.Value) bool {
	other, ok := o.(ptrPrefixValue)
	if !ok {
		return false
	}

	return v.StringValue.Equal(other.StringValue)
}

// ToStringValue satisfies basetypes.StringValuable.
func (v ptrPrefixValue) ToStringValue(_ context.Context) (basetypes.StringValue, diag.Diagnostics) {
	return v.StringValue, nil
}

// StringSemanticEquals returns true when both prefix strings canonicalise to
// the same CIDR. Canonicalisation errors fall back to literal equality.
func (v ptrPrefixValue) StringSemanticEquals(_ context.Context, other basetypes.StringValuable) (bool, diag.Diagnostics) {
	otherPfx, ok := other.(ptrPrefixValue)
	if !ok {
		return false, nil
	}

	canonV, errV := canonicalizePTRPrefix(v.ValueString())
	canonO, errO := canonicalizePTRPrefix(otherPfx.ValueString())

	if errV != nil || errO != nil {
		// Fall back to literal comparison when either value is unparseable.
		return v.ValueString() == otherPfx.ValueString(), nil
	}

	return canonV == canonO, nil
}

// canonicalizePTRPrefix converts any accepted prefix form to the canonical
// CIDR string that ptrZoneFacts would emit for the corresponding arpa zone.
//
// Accepted forms:
//
//	IPv4 bare octets: "185.181.63"   → "185.181.63.0/24"
//	                  "185.181"      → "185.181.0.0/16"
//	                  "185"          → "185.0.0.0/8"
//	IPv4 CIDR:        "185.181.63.0/24" → "185.181.63.0/24" (pass-through)
//	IPv6 bare addr:   "2a03:94e0::" → "2a03:94e0::/32" (length derived from significant nibbles)
//	IPv6 CIDR:        "2a03:94e0::/32" → "2a03:94e0::/32" (pass-through)
//
// The function returns an error for any form it cannot interpret; callers
// treat errors as "not equal" and fall back to literal comparison.
func canonicalizePTRPrefix(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("canonicalizePTRPrefix: empty prefix")
	}

	// If it contains a '/' it is already in CIDR notation — parse and
	// normalise via net/netip.
	if strings.ContainsRune(s, '/') {
		pfx, err := netip.ParsePrefix(s)
		if err != nil {
			return "", fmt.Errorf("canonicalizePTRPrefix: invalid CIDR %q: %w", s, err)
		}
		// Mask to network address (same as net.ParseCIDR does) and return.
		masked := pfx.Masked()

		return masked.String(), nil
	}

	// IPv6 detection: contains ':'.
	if strings.ContainsRune(s, ':') {
		return canonicalizeBarePTRPrefixIPv6(s)
	}

	// Otherwise treat as IPv4 bare-octet form.
	return canonicalizeBarePTRPrefixIPv4(s)
}

// canonicalizeBarePTRPrefixIPv4 handles "185.181.63" style inputs.
func canonicalizeBarePTRPrefixIPv4(s string) (string, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return "", fmt.Errorf("canonicalizePTRPrefix: IPv4 bare form %q must have 1–3 octets", s)
	}

	bits := len(parts) * 8

	// Pad to 4 octets with zeros.
	for len(parts) < 4 {
		parts = append(parts, "0")
	}

	cidr := fmt.Sprintf("%s/%d", strings.Join(parts, "."), bits)

	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return "", fmt.Errorf("canonicalizePTRPrefix: derived CIDR %q is invalid: %w", cidr, err)
	}

	return cidr, nil
}

// canonicalizeBarePTRPrefixIPv6 handles "2a03:94e0::" style inputs where
// no prefix length is given. The prefix length is derived by counting the
// nibbles in the explicitly-specified portion of the IPv6 address (the part
// before "::" or the full address if "::" is absent) and multiplying by 4.
// This matches ptrZoneFactsIPv6 which derives prefix length from the nibble
// count in the corresponding ip6.arpa zone name.
//
// Examples:
//
//	"2a03:94e0::" → groups ["2a03","94e0"] → 8 nibbles → /32
//	"2a03::"      → groups ["2a03"]        → 4 nibbles → /16
func canonicalizeBarePTRPrefixIPv6(s string) (string, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return "", fmt.Errorf("canonicalizePTRPrefix: %q is not a valid IPv6 address", s)
	}

	ip = ip.To16()
	if ip == nil {
		return "", fmt.Errorf("canonicalizePTRPrefix: %q could not be converted to 16-byte form", s)
	}

	// Determine how many nibbles are explicitly written in the prefix string.
	// Split on "::" and take the left-hand side; if no "::" is present, use
	// the whole string. Then count nibbles across all colon-delimited groups.
	explicit := s
	if before, _, ok := strings.Cut(s, "::"); ok {
		explicit = before
	}

	groups := 0

	if explicit != "" {
		for range strings.SplitSeq(explicit, ":") {
			// Each colon-separated group represents one 16-bit word = 4 nibbles.
			// This matches the arpa zone naming convention where each 16-bit
			// group yields 4 nibble labels in the ip6.arpa name.
			groups++
		}
	}

	// Each group is 16 bits (4 nibbles).
	bits := groups * 16

	cidr := fmt.Sprintf("%s/%d", ip.String(), bits)

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("canonicalizePTRPrefix: derived CIDR %q is invalid: %w", cidr, err)
	}

	return fmt.Sprintf("%s/%d", ipNet.IP.String(), bits), nil
}
