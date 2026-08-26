package tfprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ basetypes.StringTypable                    = sshPublicKeyType{}
	_ basetypes.StringValuableWithSemanticEquals = sshPublicKeyValue{}
)

// sshPublicKeyType is a custom string type for SSH public keys. It
// produces sshPublicKeyValue values that implement semantic equality,
// treating two keys as equal when they differ only in surrounding
// whitespace (e.g. a trailing newline from file("key.pub")).
type sshPublicKeyType struct {
	basetypes.StringType
}

func (t sshPublicKeyType) Equal(o attr.Type) bool {
	_, ok := o.(sshPublicKeyType)

	return ok
}

func (t sshPublicKeyType) String() string {
	return "sshPublicKeyType"
}

func (t sshPublicKeyType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return sshPublicKeyValue{StringValue: in}, nil
}

func (t sshPublicKeyType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	attrValue, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}

	sv, ok := attrValue.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", attrValue)
	}

	return sshPublicKeyValue{StringValue: sv}, nil
}

func (t sshPublicKeyType) ValueType(_ context.Context) attr.Value {
	return sshPublicKeyValue{}
}

// sshPublicKeyValue is the value type for sshPublicKeyType. Two values are
// semantically equal when their trimmed representations are identical, so
// whitespace-only differences (e.g. trailing newlines) never produce a diff.
type sshPublicKeyValue struct {
	basetypes.StringValue
}

func (v sshPublicKeyValue) Type(_ context.Context) attr.Type {
	return sshPublicKeyType{}
}

func (v sshPublicKeyValue) Equal(o attr.Value) bool {
	other, ok := o.(sshPublicKeyValue)
	if !ok {
		return false
	}

	return v.StringValue.Equal(other.StringValue)
}

// ToStringValue satisfies basetypes.StringValuable.
func (v sshPublicKeyValue) ToStringValue(_ context.Context) (basetypes.StringValue, diag.Diagnostics) {
	return v.StringValue, nil
}

// StringSemanticEquals returns true when both keys are equal after trimming
// surrounding whitespace.
func (v sshPublicKeyValue) StringSemanticEquals(_ context.Context, other basetypes.StringValuable) (bool, diag.Diagnostics) {
	otherSV, ok := other.(sshPublicKeyValue)
	if !ok {
		return false, nil
	}

	return strings.TrimSpace(v.ValueString()) == strings.TrimSpace(otherSV.ValueString()), nil
}
