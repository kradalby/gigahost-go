package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"go.yaml.in/yaml/v3"
)

// outputFormat controls how command results are rendered.
type outputFormat string

const (
	outputTable outputFormat = "table"
	outputJSON  outputFormat = "json"
	outputYAML  outputFormat = "yaml"
)

// parseOutputFormat validates and returns a typed output format.
func parseOutputFormat(s string) (outputFormat, error) {
	switch strings.ToLower(s) {
	case "", "table":
		return outputTable, nil
	case "json":
		return outputJSON, nil
	case "yaml", "yml":
		return outputYAML, nil
	}

	return "", fmt.Errorf("unknown output format %q: want table, json or yaml", s)
}

// render writes v to w using the requested format. For table output the
// caller must pass a slice whose element is a struct; columns come from
// the first row's exported field names.
func render(w io.Writer, format outputFormat, v any) error {
	switch format {
	case outputJSON:
		return renderJSON(w, v)
	case outputYAML:
		return renderYAML(w, v)
	case outputTable:
		return renderTable(w, v)
	}

	return fmt.Errorf("unsupported format %q", format)
}

func renderJSON(w io.Writer, v any) error {
	enc := json.MarshalWrite(w, v)
	if enc != nil {
		return enc
	}

	_, err := fmt.Fprintln(w)

	return err
}

func renderYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)

	if err := enc.Encode(v); err != nil {
		return err
	}

	return enc.Close()
}

// renderTable uses reflection to build a table from a slice of structs
// or a single struct. Primitive types are passed through directly.
func renderTable(w io.Writer, v any) error {
	if v == nil {
		return errors.New("nil value")
	}

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)

	switch rv.Kind() { //nolint:exhaustive // only slice/array/struct are meaningful for table rendering
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			_, err := fmt.Fprintln(w, "(no results)")

			return err
		}

		first := rv.Index(0)
		headers, extract := structSchema(first)
		t.AppendHeader(headers)

		for i := range rv.Len() {
			t.AppendRow(extract(rv.Index(i)))
		}

		t.Render()

		return nil
	case reflect.Struct:
		headers, extract := structSchema(rv)
		t.AppendHeader(headers)
		t.AppendRow(extract(rv))
		t.Render()

		return nil
	default:
		_, err := fmt.Fprintln(w, rv.Interface())

		return err
	}
}

// structSchema returns the column headers and a value extractor for a
// struct's exported fields. Fields with the tag `cli:"-"` are skipped.
func structSchema(sv reflect.Value) (table.Row, func(reflect.Value) table.Row) {
	for sv.Kind() == reflect.Pointer {
		sv = sv.Elem()
	}

	t := sv.Type()

	var (
		headers = table.Row{}
		indexes = []int{}
	)

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		tag := f.Tag.Get("cli")
		if tag == "-" {
			continue
		}

		name := tag
		if name == "" {
			name = f.Name
		}

		headers = append(headers, name)
		indexes = append(indexes, i)
	}

	extract := func(row reflect.Value) table.Row {
		for row.Kind() == reflect.Pointer {
			row = row.Elem()
		}

		r := make(table.Row, 0, len(indexes))
		for _, idx := range indexes {
			r = append(r, row.Field(idx).Interface())
		}

		return r
	}

	return headers, extract
}
