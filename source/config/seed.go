package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Seed writes a default configuration file at path holding every key of struct type T at its value
// in defaults, and reports whether it created the file.
//
// The file is YAML that File reads back to defaults: a nested struct is a nested mapping under its
// key, keys are in the order the struct declares its fields, and each value is written as
// Source.Value describes it, a slice as a list of its elements, so that an element holding a ","
// stays one element, and a nil or empty slice as "[]". A string is quoted wherever YAML would
// otherwise read it as something else, such as "", "yes" or "123". The `doc:"..."` tag of a field
// is a comment above its key, and that of a nested struct field a comment above its mapping.
//
// Seed creates the file with mode 0600 and never replaces one: when path already exists, Seed
// leaves it as it is and returns false with no error. It creates no directory, so a missing parent
// directory is an error. When the file cannot be written in full, Seed removes it, leaving no
// partial file behind.
//
// T is a struct, not a pointer, and its Schema is reflected as NewSchema describes; an invalid
// Schema is returned as NewSchema reports it, matching ErrSchema, and nothing is written.
func Seed[T any](path string, defaults T) (created bool, err error) {
	if t := reflect.TypeFor[T](); t.Kind() != reflect.Struct {
		return false, fmt.Errorf("%w: configuration type is %v, not a struct", ErrSchema, t)
	}
	schema, err := NewSchema(defaults, "")
	if err != nil {
		return false, err
	}
	data, err := renderDefaults(schema, reflect.ValueOf(defaults))
	if err != nil {
		return false, fmt.Errorf("config file %s: %w", path, err)
	}
	return writeNew(path, data, createExclusive)
}

// createExclusive creates the file at path for writing with mode 0600, failing with an error
// matching fs.ErrExist when anything, a dangling symbolic link included, is already at path.
func createExclusive(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// writeNew writes data to a file create makes at path, and reports whether it made one. An error
// matching fs.ErrExist from create means the file exists, which is not a failure. When the write or
// the close fails, the file is removed.
func writeNew(path string, data []byte, create func(string) (io.WriteCloser, error)) (bool, error) {
	f, err := create(path)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config file %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("config file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("config file %s: %w", path, err)
	}
	return true, nil
}

// renderDefaults returns the YAML document holding every field of schema at its value in v, a
// struct of the type schema was reflected from.
func renderDefaults(schema *Schema, v reflect.Value) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	groups := map[string]*yaml.Node{"": root}
	for _, f := range schema.fields {
		segments := strings.Split(f.key, ".")
		parent := root
		for n := 1; n < len(segments); n++ {
			group := strings.Join(segments[:n], ".")
			mapping, ok := groups[group]
			if !ok {
				mapping = &yaml.Node{Kind: yaml.MappingNode}
				key := &yaml.Node{Kind: yaml.ScalarNode, Value: segments[n-1], HeadComment: comment(schema.GroupDoc(group))}
				parent.Content = append(parent.Content, key, mapping)
				groups[group] = mapping
			}
			parent = mapping
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: segments[len(segments)-1], HeadComment: comment(f.doc)}
		parent.Content = append(parent.Content, key, valueNode(v.FieldByIndex(f.index)))
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// valueNode returns the YAML node of v, the value of a field: a list of its elements for a slice,
// written in flow style when it has none, and otherwise a scalar.
func valueNode(v reflect.Value) *yaml.Node {
	if v.Kind() != reflect.Slice {
		return scalarNode(v)
	}
	list := &yaml.Node{Kind: yaml.SequenceNode}
	if v.Len() == 0 {
		list.Style = yaml.FlowStyle
	}
	for i := range v.Len() {
		list.Content = append(list.Content, scalarNode(v.Index(i)))
	}
	return list
}

// scalarNode returns the YAML scalar of v, a single value, as formatValue writes it. A string is
// tagged as one, so that the encoder quotes any text YAML would resolve to another type or to null;
// the file loader reads the text either way, but reads a null as no value at all.
func scalarNode(v reflect.Value) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: formatValue(v)}
	if v.Kind() == reflect.String {
		n.Tag = "!!str"
	}
	return n
}

// comment returns doc as a YAML comment, each of its lines starting with "#", or "" when doc is
// empty.
func comment(doc string) string {
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = "#"
		} else {
			lines[i] = "# " + line
		}
	}
	return strings.Join(lines, "\n")
}
