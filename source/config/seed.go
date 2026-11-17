package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
// A collection is written as its elements: a list of structs in order, a map as a mapping with its
// keys sorted and written verbatim, and each element as the configuration is, every one of its
// fields at its value. Only the first element of a collection, and the first entry of a map at each
// level, carries the docs of the element's fields. A nil or empty collection is "[]" or "{}", which
// File reads back as an empty one, and its comment is followed by a commented-out example of the key
// holding one element: each field of the element at its zero value with its doc, a map's entry
// under the placeholder key "<name>", and a collection within the element as its own example. A map
// of single values has no element, and its example is one zero value under "<name>".
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
// matching fs.ErrExist when anything, a dangling symbolic link included, is already at path. The
// file is opened at filepath.Clean(path).
func createExclusive(path string) (io.WriteCloser, error) {
	return os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
	return encode(structNode(schema, v, true, false))
}

// encode returns the YAML text of node, a mapping, indented by two spaces.
func encode(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{node}})
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// examplePlaceholder is the key of the entry in the example of an empty map.
const examplePlaceholder = "<name>"

// structNode returns the mapping holding every field of schema at its value in v, a struct of the
// type schema was reflected from: the configuration or an element of a collection. Each key carries
// its doc, and each nested struct's mapping the doc of its group, only when docs is set; the doc of
// an empty collection is followed by its example. When example is set, every collection is written
// as its example rather than its value.
func structNode(schema *Schema, v reflect.Value, docs, example bool) *yaml.Node {
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
				key := &yaml.Node{Kind: yaml.ScalarNode, Value: segments[n-1]}
				if docs {
					key.HeadComment = comment(schema.GroupDoc(group))
				}
				parent.Content = append(parent.Content, key, mapping)
				groups[group] = mapping
			}
			parent = mapping
		}
		name := segments[len(segments)-1]
		fv := v.FieldByIndex(f.index)
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
		value := valueNode(fv)
		switch {
		case f.kind != KindValue && example:
			value = exampleNode(f.typ, f.elem)
		case f.kind != KindValue:
			value = collectionNode(f.typ, f.elem, fv, docs)
		}
		if docs {
			key.HeadComment = comment(f.doc)
			if f.kind != KindValue && !example && fv.Len() == 0 {
				key.HeadComment = withExample(key.HeadComment, name, f)
			}
		}
		parent.Content = append(parent.Content, key, value)
	}
	return root
}

// collectionNode returns the YAML node of v, a value of type t within a collection whose element
// has the Schema elem, nil when it holds no struct. A map is a mapping of its entries sorted by key,
// a slice of structs a list of its elements in order, and either one in flow style, "{}" or "[]",
// when it has none. Only the first entry or element at each level carries docs, and only when docs
// is set.
func collectionNode(t reflect.Type, elem *Schema, v reflect.Value, docs bool) *yaml.Node {
	switch {
	case t.Kind() == reflect.Map:
		mapping := &yaml.Node{Kind: yaml.MappingNode}
		if v.Len() == 0 {
			mapping.Style = yaml.FlowStyle
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for i, k := range keys {
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.String()}
			mapping.Content = append(mapping.Content, key, collectionNode(t.Elem(), elem, v.MapIndex(k), docs && i == 0))
		}
		return mapping
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		list := &yaml.Node{Kind: yaml.SequenceNode}
		if v.Len() == 0 {
			list.Style = yaml.FlowStyle
		}
		for i := range v.Len() {
			list.Content = append(list.Content, structNode(elem, v.Index(i), docs && i == 0, false))
		}
		return list
	case t.Kind() == reflect.Struct:
		return structNode(elem, v, docs, false)
	default:
		return valueNode(v)
	}
}

// withExample returns head, the comment above the key name of f, an empty collection, followed by
// a commented-out example of name holding one element, each element field at its zero value with its
// doc. A map's example entry is under the placeholder key "<name>"; a map of single values holds one
// zero value there. A blank comment line separates the example from head.
//
// The example holds only field names, the placeholder, zero values and docs, which the encoder writes
// as comments without checking them, so encoding it never fails.
func withExample(head, name string, f Field) string {
	example := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: name},
		exampleNode(f.typ, f.elem),
	}}
	text, _ := encode(example)
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	for i, line := range lines {
		lines[i] = "# " + line
	}
	if head != "" {
		head += "\n#\n"
	}
	return head + strings.Join(lines, "\n")
}

// exampleNode returns the YAML node of one example value of type t, within a collection whose
// element has the Schema elem: a map holds one entry under the key "<name>", a slice of structs one
// element, and an element every field of elem with its doc, at its zero value or, for a collection,
// at its own example.
func exampleNode(t reflect.Type, elem *Schema) *yaml.Node {
	switch {
	case t.Kind() == reflect.Map:
		return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: examplePlaceholder},
			exampleNode(t.Elem(), elem),
		}}
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		return &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{exampleNode(t.Elem(), elem)}}
	case t.Kind() == reflect.Struct:
		return structNode(elem, reflect.New(t).Elem(), true, true)
	default:
		return valueNode(reflect.New(t).Elem())
	}
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
