package tfprovider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Value constructors for the harness. Building tftypes values by hand is
// verbose enough that tests stop being readable without these.

func tfStr(v string) tftypes.Value  { return tftypes.NewValue(tftypes.String, v) }
func tfNum(v float64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
func tfBool(v bool) tftypes.Value   { return tftypes.NewValue(tftypes.Bool, v) }

// tfStrList builds a list of strings, e.g. for ssh_keys.
func tfStrList(vals ...string) tftypes.Value {
	out := make([]tftypes.Value, 0, len(vals))
	for _, v := range vals {
		out = append(out, tfStr(v))
	}

	return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, out)
}

// mkObject builds a value for objType, defaulting every attribute the caller
// did not set to null. Tests then name only the attributes they care about.
func mkObject(objType tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	out := make(map[string]tftypes.Value, len(objType.AttributeTypes))

	for k, attrType := range objType.AttributeTypes {
		if v, ok := set[k]; ok {
			out[k] = v

			continue
		}

		out[k] = tftypes.NewValue(attrType, nil)
	}

	return tftypes.NewValue(objType, out)
}

// nullObject is the prior state for a create: the whole object is null.
func nullObject(objType tftypes.Object) tftypes.Value {
	return tftypes.NewValue(objType, nil)
}

// emptyList builds an empty list of the given attribute's type, for computed
// list attributes such as ips that must be present but carry no elements.
func emptyList(objType tftypes.Object, attr string) tftypes.Value {
	return tftypes.NewValue(objType.AttributeTypes[attr], []tftypes.Value{})
}

// proposedNew approximates terraform core's objchange.ProposedNew for the flat
// attribute schemas this provider uses: an attribute the config leaves null
// keeps its prior value, everything else takes the config value. Terraform
// core computes this before calling PlanResourceChange, so the harness has to
// supply it too.
func proposedNew(t *testing.T, objType tftypes.Object, prior, config tftypes.Value) tftypes.Value {
	t.Helper()

	if prior.IsNull() {
		return config
	}

	priorMap := asMap(t, prior)
	configMap := asMap(t, config)

	out := make(map[string]tftypes.Value, len(objType.AttributeTypes))

	for k := range objType.AttributeTypes {
		if v, ok := configMap[k]; ok && !v.IsNull() {
			out[k] = v

			continue
		}

		if v, ok := priorMap[k]; ok {
			out[k] = v

			continue
		}

		out[k] = tftypes.NewValue(objType.AttributeTypes[k], nil)
	}

	return tftypes.NewValue(objType, out)
}

// asMap destructures an object value into its attributes.
func asMap(t *testing.T, v tftypes.Value) map[string]tftypes.Value {
	t.Helper()

	if v.IsNull() || !v.IsKnown() {
		return nil
	}

	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		t.Fatalf("destructure object: %v", err)
	}

	return m
}

// str reads a string attribute, reporting "" for null or unknown.
func str(m map[string]tftypes.Value, attr string) string {
	v, ok := m[attr]
	if !ok || v.IsNull() || !v.IsKnown() {
		return ""
	}

	var s string
	if err := v.As(&s); err != nil {
		return ""
	}

	return s
}

func hasError(diags []*tfprotov6.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}

	return false
}

func diagText(diags []*tfprotov6.Diagnostic, sev tfprotov6.DiagnosticSeverity) string {
	var b strings.Builder

	for _, d := range diags {
		if d.Severity != sev {
			continue
		}

		if b.Len() > 0 {
			b.WriteString("; ")
		}

		b.WriteString(d.Summary)

		if d.Detail != "" {
			b.WriteString(": ")
			b.WriteString(d.Detail)
		}
	}

	return b.String()
}

// failOnError fails the test when diags carries an error, naming the call.
func failOnError(t *testing.T, what string, diags []*tfprotov6.Diagnostic) {
	t.Helper()

	if hasError(diags) {
		t.Fatalf("%s: %s", what, diagText(diags, tfprotov6.DiagnosticSeverityError))
	}
}
