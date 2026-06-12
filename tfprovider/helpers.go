package tfprovider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gigahost "github.com/kradalby/gigahost-go/client"
)

// stringOrNull wraps a string as a Terraform value, mapping the empty string to
// null rather than an empty string so absent optional API fields (e.g. an
// unassigned IPv6 address) do not round-trip as "".
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}

	return types.StringValue(s)
}

// parseImportID splits a composite import ID into exactly len(names)
// non-empty parts and returns a user-facing error naming the expected
// format ("<zone>/<record_id>") otherwise.
func parseImportID(id string, names ...string) ([]string, error) {
	invalid := func() error {
		format := "<" + strings.Join(names, ">/<") + ">"

		return fmt.Errorf("invalid import ID %q: expected %s", id, format)
	}

	parts := strings.Split(id, "/")

	if len(parts) != len(names) {
		return nil, invalid()
	}

	if slices.Contains(parts, "") {
		return nil, invalid()
	}

	return parts, nil
}

// normalizeASNImportID normalises a raw ASN import identifier.
// It accepts "212345", "AS212345" and "as212345" (with optional surrounding
// whitespace) and returns the bare numeric string. Any other form is an error.
func normalizeASNImportID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("invalid ASN %q: accepted forms are 212345 or AS212345", raw)
	}

	if strings.HasPrefix(strings.ToUpper(s), "AS") {
		s = s[2:]
	}

	if s == "" {
		return "", fmt.Errorf("invalid ASN %q: accepted forms are 212345 or AS212345", raw)
	}

	for _, r := range s {
		if !unicode.IsDigit(r) {
			return "", fmt.Errorf("invalid ASN %q: accepted forms are 212345 or AS212345", raw)
		}
	}

	return s, nil
}

// findZoneID resolves a zone identifier (opaque zone ID or zone name,
// case-insensitive) against a zone listing. The opaque ID stays canonical.
func findZoneID(zones []gigahost.Zone, identifier string) (string, error) {
	lower := strings.ToLower(identifier)

	for _, z := range zones {
		if z.ID == identifier {
			return z.ID, nil
		}
	}

	for _, z := range zones {
		if strings.ToLower(z.Name) == lower {
			return z.ID, nil
		}
	}

	return "", fmt.Errorf("zone %q not found: provide the zone ID or zone name", identifier)
}

// resolveZoneIdentifier lists zones once and resolves identifier via findZoneID.
// Used by import handlers that accept a zone ID or name.
func resolveZoneIdentifier(ctx context.Context, c *gigahost.Client, identifier string) (string, error) {
	zones, err := c.DNS.ListZones(ctx)
	if err != nil {
		return "", fmt.Errorf("list zones: %w", err)
	}

	return findZoneID(zones, identifier)
}

// listToStrings materialises a types.List of types.String into a
// plain []string. It returns diagnostics for the caller to append.
func listToStrings(ctx context.Context, l types.List) ([]string, diag.Diagnostics) {
	if l.IsNull() || l.IsUnknown() {
		return nil, nil
	}

	out := make([]string, 0, len(l.Elements()))
	diags := l.ElementsAs(ctx, &out, false)

	return out, diags
}

// planDSRecords converts a planned types.List into the API's
// [gigahost.DSRecord] slice.
func planDSRecords(ctx context.Context, l types.List) ([]gigahost.DSRecord, diag.Diagnostics) {
	if l.IsNull() || l.IsUnknown() {
		return nil, nil
	}

	models := make([]dsRecordModel, 0, len(l.Elements()))

	if diags := l.ElementsAs(ctx, &models, false); diags.HasError() {
		return nil, diags
	}

	out := make([]gigahost.DSRecord, 0, len(models))
	for _, m := range models {
		out = append(out, gigahost.DSRecord{
			KeyTag:     int(m.KeyTag.ValueInt64()),
			Algorithm:  int(m.Algorithm.ValueInt64()),
			DigestType: int(m.DigestType.ValueInt64()),
			Digest:     m.Digest.ValueString(),
		})
	}

	return out, nil
}

// dsRecordsToList converts a [gigahost.DSRecord] slice back into a
// Terraform types.List for state updates.
func dsRecordsToList(ctx context.Context, records []gigahost.DSRecord) (types.List, diag.Diagnostics) {
	models := make([]dsRecordModel, 0, len(records))

	for _, r := range records {
		models = append(models, dsRecordModel{
			KeyTag:     types.Int64Value(int64(r.KeyTag)),
			Algorithm:  types.Int64Value(int64(r.Algorithm)),
			DigestType: types.Int64Value(int64(r.DigestType)),
			Digest:     types.StringValue(r.Digest),
		})
	}

	return types.ListValueFrom(ctx, dsRecordObjectType, models)
}
