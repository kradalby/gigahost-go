package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ basetypes.StringTypable                    = asnType{}
	_ basetypes.StringValuableWithSemanticEquals = asnValue{}
)

// asnType is a custom string type for Autonomous System Numbers. It produces
// asnValue values that implement semantic equality, treating bare numeric
// ("212345"), AS-prefixed ("AS212345"), and case-variant ("as212345") forms
// as identical. Whitespace is ignored. The canonical (stored) form is the bare
// numeric string emitted by the API.
type asnType struct {
	basetypes.StringType
}

func (t asnType) Equal(o attr.Type) bool {
	_, ok := o.(asnType)

	return ok
}

func (t asnType) String() string {
	return "asnType"
}

func (t asnType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return asnValue{StringValue: in}, nil
}

func (t asnType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	attrValue, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}

	sv, ok := attrValue.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", attrValue)
	}

	return asnValue{StringValue: sv}, nil
}

func (t asnType) ValueType(_ context.Context) attr.Value {
	return asnValue{}
}

// asnValue is the value type for asnType. Two ASN values are semantically
// equal when they normalise to the same bare numeric string via
// normalizeASNImportID. If either value fails to normalise, equality falls
// back to a literal string comparison.
type asnValue struct {
	basetypes.StringValue
}

func (v asnValue) Type(_ context.Context) attr.Type {
	return asnType{}
}

func (v asnValue) Equal(o attr.Value) bool {
	other, ok := o.(asnValue)
	if !ok {
		return false
	}

	return v.StringValue.Equal(other.StringValue)
}

// ToStringValue satisfies basetypes.StringValuable.
func (v asnValue) ToStringValue(_ context.Context) (basetypes.StringValue, diag.Diagnostics) {
	return v.StringValue, nil
}

// StringSemanticEquals returns true when both ASN strings normalise to the
// same bare numeric value. Normalization errors fall back to literal equality.
func (v asnValue) StringSemanticEquals(_ context.Context, other basetypes.StringValuable) (bool, diag.Diagnostics) {
	otherASN, ok := other.(asnValue)
	if !ok {
		return false, nil
	}

	normV, errV := normalizeASNImportID(v.ValueString())
	normO, errO := normalizeASNImportID(otherASN.ValueString())

	if errV != nil || errO != nil {
		// Fall back to literal comparison when either value is unparseable.
		return v.ValueString() == otherASN.ValueString(), nil
	}

	return normV == normO, nil
}
