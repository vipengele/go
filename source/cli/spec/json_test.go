package spec_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

// ordered marshals its fields in declaration order, which is not alphabetical.
type ordered struct {
	Zulu    string `json:"zulu"`
	Alpha   int    `json:"alpha"`
	Mike    bool   `json:"mike"`
	Charlie any    `json:"charlie"`
}

func TestWriteJSONWritesSchemaFirstThenKeysInOrder(t *testing.T) {
	var buf bytes.Buffer
	v := ordered{Zulu: "z", Alpha: 1, Mike: true, Charlie: nil}
	if err := spec.WriteJSON(&buf, 3, v); err != nil {
		t.Fatalf("WriteJSON() = %v, want nil", err)
	}
	want := "{\n" +
		"  \"schema\": 3,\n" +
		"  \"zulu\": \"z\",\n" +
		"  \"alpha\": 1,\n" +
		"  \"mike\": true,\n" +
		"  \"charlie\": null\n" +
		"}\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteJSON() wrote\n%s\nwant\n%s", got, want)
	}
}

func TestWriteJSONEndsWithOneNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := spec.WriteJSON(&buf, 1, ordered{}); err != nil {
		t.Fatalf("WriteJSON() = %v, want nil", err)
	}
	got := buf.String()
	if !strings.HasSuffix(got, "}\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("WriteJSON() wrote %q, want it to end with exactly one newline after the object", got)
	}
}

func TestWriteJSONEmptyObjectHoldsOnlySchema(t *testing.T) {
	var buf bytes.Buffer
	if err := spec.WriteJSON(&buf, 1, struct{}{}); err != nil {
		t.Fatalf("WriteJSON() = %v, want nil", err)
	}
	want := "{\n  \"schema\": 1\n}\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteJSON() wrote %q, want %q", got, want)
	}
}

func TestWriteJSONIndentsNestedValues(t *testing.T) {
	var buf bytes.Buffer
	v := ordered{
		Zulu: "z",
		Charlie: struct {
			List  []int          `json:"list"`
			Inner map[string]int `json:"inner"`
		}{List: []int{1, 2}, Inner: map[string]int{"k": 7}},
	}
	if err := spec.WriteJSON(&buf, 2, v); err != nil {
		t.Fatalf("WriteJSON() = %v, want nil", err)
	}
	want := "{\n" +
		"  \"schema\": 2,\n" +
		"  \"zulu\": \"z\",\n" +
		"  \"alpha\": 0,\n" +
		"  \"mike\": false,\n" +
		"  \"charlie\": {\n" +
		"    \"list\": [\n" +
		"      1,\n" +
		"      2\n" +
		"    ],\n" +
		"    \"inner\": {\n" +
		"      \"k\": 7\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteJSON() wrote\n%s\nwant\n%s", got, want)
	}

	var decoded struct {
		Schema  int `json:"schema"`
		Charlie struct {
			List  []int          `json:"list"`
			Inner map[string]int `json:"inner"`
		} `json:"charlie"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output does not decode: %v", err)
	}
	if decoded.Schema != 2 || len(decoded.Charlie.List) != 2 || decoded.Charlie.Inner["k"] != 7 {
		t.Errorf("decoded output = %+v, want schema 2 and the nested values unchanged", decoded)
	}
}

func TestWriteJSONRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		schema int
		v      any
		want   string
	}{
		{"array", 1, []int{1, 2}, "not an object"},
		{"empty array", 1, []string{}, "not an object"},
		{"string", 1, "text", "not an object"},
		{"number", 1, 42, "not an object"},
		{"boolean", 1, true, "not an object"},
		{"null", 1, nil, "not an object"},
		{"nil map", 1, map[string]int(nil), "not an object"},
		{"schema zero", 0, ordered{}, "schema 0"},
		{"schema negative", -1, ordered{}, "schema -1"},
		{"value has schema", 1, struct {
			Schema int `json:"schema"`
		}{Schema: 1}, `"schema" key`},
		{"value has escaped schema", 1, json.RawMessage(`{"a":1,"schema":2}`), `"schema" key`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := spec.WriteJSON(&buf, tc.schema, tc.v)
			if err == nil {
				t.Fatalf("WriteJSON() = nil, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("WriteJSON() = %q, want it to contain %q", err, tc.want)
			}
			if buf.Len() != 0 {
				t.Errorf("WriteJSON() wrote %q on error, want nothing", buf.String())
			}
		})
	}
}

func TestWriteJSONWrapsMarshalError(t *testing.T) {
	var buf bytes.Buffer
	err := spec.WriteJSON(&buf, 1, map[string]any{"f": func() {}})
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("WriteJSON() = %v, want it to wrap a *json.UnsupportedTypeError", err)
	}
	if buf.Len() != 0 {
		t.Errorf("WriteJSON() wrote %q on error, want nothing", buf.String())
	}
}

// countingFailWriter counts its calls and fails each one with err.
type countingFailWriter struct {
	err   error
	calls int
}

func (w *countingFailWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestWriteJSONReturnsWriteError(t *testing.T) {
	want := errors.New("write failed")
	w := &countingFailWriter{err: want}
	err := spec.WriteJSON(w, 1, ordered{})
	if !errors.Is(err, want) {
		t.Errorf("WriteJSON() = %v, want it to wrap %v", err, want)
	}
	if w.calls != 1 {
		t.Errorf("WriteJSON() called Write %d times, want 1", w.calls)
	}
}
