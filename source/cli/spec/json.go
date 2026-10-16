package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// schemaKey is the key of the envelope's schema version.
const schemaKey = "schema"

// WriteJSON writes v to w as one indented JSON object whose first key is "schema", set to schema,
// followed by the keys of v in the order v marshals them, and a trailing newline. A run function
// passes inv.Env.Stdout as w.
//
// v must marshal to a JSON object that has no "schema" key of its own, and schema must be 1 or
// more; otherwise WriteJSON returns an error and writes nothing. The output is written in one call
// to w, after v is marshalled and checked, so a rejected value never leaves partial output.
func WriteJSON(w io.Writer, schema int, v any) error {
	if schema < 1 {
		return fmt.Errorf("spec: JSON schema %d is not 1 or more", schema)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("spec: marshal JSON value: %w", err)
	}
	if err := checkEnvelopeObject(data); err != nil {
		return err
	}

	// json.Marshal writes compact JSON, so data is "{}" or "{" followed by the first key.
	var spliced []byte
	spliced = append(spliced, `{"`+schemaKey+`":`...)
	spliced = strconv.AppendInt(spliced, int64(schema), 10)
	if len(data) > len("{}") {
		spliced = append(spliced, ',')
	}
	spliced = append(spliced, data[1:]...)

	var out bytes.Buffer
	if err := json.Indent(&out, spliced, "", "  "); err != nil {
		return fmt.Errorf("spec: indent JSON value: %w", err)
	}
	out.WriteByte('\n')
	if _, err := w.Write(out.Bytes()); err != nil {
		return fmt.Errorf("spec: write JSON: %w", err)
	}
	return nil
}

// checkEnvelopeObject reports an error unless data is a JSON object without a "schema" key.
// Keys are compared after unescaping, so an escaped spelling of "schema" is a "schema" key.
func checkEnvelopeObject(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("spec: decode JSON value: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("spec: JSON value is %s, not an object", describeJSON(data))
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("spec: decode JSON key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("spec: JSON object key is not a string")
		}
		if key == schemaKey {
			return fmt.Errorf("spec: JSON value already has a %q key", schemaKey)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return fmt.Errorf("spec: decode JSON value of %q: %w", key, err)
		}
	}
	return nil
}

// describeJSON names the kind of the JSON value data holds, for an error message.
func describeJSON(data []byte) string {
	switch data[0] {
	case '[':
		return "an array"
	case '"':
		return "a string"
	case 'n':
		return "null"
	case 't', 'f':
		return "a boolean"
	default:
		return "a number"
	}
}
