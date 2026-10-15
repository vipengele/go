package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// ErrSchema is matched, through errors.Is, by every error NewSchema returns.
var ErrSchema = errors.New("config: invalid schema")

// Schema is the set of keys a configuration struct accepts, reflected from its fields. A Provider
// reads it to learn which keys exist and under which environment variable and flag each one is
// found.
//
// Each exported field of the struct is one key. Its segment is the name in its `config:"name"`
// tag, or, without one, its field name, in snake_case either way: `config:"maxRetries"` and an
// untagged MaxRetries both give max_retries. A field of struct type is a level of nesting whose
// fields become dotted keys below its own segment ("server.max_retries"). A field tagged
// `config:"-"`, and an unexported field, has no key.
//
// A key holds a bool, a signed or unsigned integer, a float, a string or a time.Duration, or a
// slice of one of these. A pointer field, an embedded field, a map and any other type is a schema
// error, as is a nested struct with no keys.
type Schema struct {
	fields []Field
	byKey  map[string]int
}

// Field is one key of a Schema.
type Field struct {
	key   string
	env   string
	flag  string
	typ   reflect.Type
	index []int
}

// Key returns the canonical dotted key: snake_case segments joined by ".", such as
// "server.max_retries".
func (f Field) Key() string { return f.key }

// Env returns the environment variable of the key: the Schema's prefix, an underscore, and the key
// upper-cased with "." replaced by "_", such as APP_SERVER_MAX_RETRIES. Without a prefix it is the
// upper-cased key alone.
func (f Field) Env() string { return f.env }

// Flag returns the command-line flag name of the key, without leading dashes: the key with "_"
// replaced by "-" and its "." kept, such as "server.max-retries".
func (f Field) Flag() string { return f.flag }

// Type returns the Go type of the struct field the key binds to.
func (f Field) Type() reflect.Type { return f.typ }

// NewSchema reflects the Schema of target, a struct or a pointer to a struct, whose environment
// variables start with envPrefix. envPrefix is empty, or an upper-case ASCII letter followed by
// upper-case letters, digits and underscores, not ending in an underscore.
//
// Every problem is reported in one error: each unsupported field, and each set of fields that
// collide. Fields collide when their keys match, ignoring case, "-" and "_"; when they derive the
// same environment variable or flag; or when one key is the group of another ("server" and
// "server.port"), which a nested source such as a file cannot hold at once.
func NewSchema(target any, envPrefix string) (*Schema, error) {
	t := reflect.TypeOf(target)
	if t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: target is %v, not a struct or a pointer to a struct", ErrSchema, reflect.TypeOf(target))
	}

	b := &schemaBuilder{}
	if !validPrefix(envPrefix) {
		b.fail(fmt.Sprintf("environment prefix %q is not an upper-case ASCII letter followed by upper-case letters, digits and \"_\", not ending in \"_\"", envPrefix))
	}
	root := t.Name()
	if root == "" {
		root = t.String()
	}
	b.walk(t, root, "", nil)
	for i := range b.fields {
		b.fields[i].env = envName(envPrefix, b.fields[i].key)
		b.fields[i].flag = flagName(b.fields[i].key)
	}
	b.collisions()
	if len(b.problems) > 0 {
		return nil, errors.Join(b.problems...)
	}

	s := &Schema{fields: b.fields, byKey: make(map[string]int, len(b.fields))}
	for i, f := range b.fields {
		s.byKey[matchKey(f.key)] = i
	}
	return s, nil
}

// Fields returns every key of the Schema, in the order the struct declares its fields, each nested
// struct's keys in place of the struct.
func (s *Schema) Fields() []Field {
	out := make([]Field, len(s.fields))
	copy(out, s.fields)
	return out
}

// Lookup returns the Field whose key matches key, ignoring case, "-" and "_" within each segment:
// "Server.MaxRetries", "server.max-retries" and "SERVER.MAX_RETRIES" all find
// "server.max_retries". A key naming a nested struct rather than a value finds nothing.
func (s *Schema) Lookup(key string) (Field, bool) {
	i, ok := s.byKey[matchKey(key)]
	if !ok {
		return Field{}, false
	}
	return s.fields[i], true
}

// schemaBuilder collects the fields of a struct and every problem found while reflecting it.
type schemaBuilder struct {
	fields   []Field
	paths    []string // paths[i] is the Go path of fields[i], such as Config.Server.Port.
	problems []error
}

func (b *schemaBuilder) fail(msg string) {
	b.problems = append(b.problems, fmt.Errorf("%w: %s", ErrSchema, msg))
}

// walk adds the keys of struct type t, whose Go path is path, below the dotted key prefix, with
// index the field index path of t from the root.
func (b *schemaBuilder) walk(t reflect.Type, path, prefix string, index []int) {
	for i := range t.NumField() {
		sf := t.Field(i)
		tag, tagged := sf.Tag.Lookup("config")
		if tag == "-" {
			continue
		}
		fieldPath := path + "." + sf.Name
		if sf.Anonymous {
			b.fail(fmt.Sprintf("field %s: embedded fields are not supported; give the field a name", fieldPath))
			continue
		}
		if !sf.IsExported() {
			continue
		}

		name := sf.Name
		if tagged && tag != "" {
			name = tag
		}
		if !validName(name) {
			b.fail(fmt.Sprintf("field %s: name %q is not a valid key segment: it must start with an ASCII letter and hold only ASCII letters, digits, \"-\" and \"_\"", fieldPath, name))
			continue
		}
		key := snakeCase(name)
		if prefix != "" {
			key = prefix + "." + key
		}
		fieldIndex := append(append([]int(nil), index...), i)

		ft := sf.Type
		switch {
		case ft.Kind() == reflect.Pointer:
			b.fail(fmt.Sprintf("field %s: pointer fields are not supported", fieldPath))
		case ft.Kind() == reflect.Map:
			b.fail(fmt.Sprintf("field %s: maps are not supported", fieldPath))
		case ft.Kind() == reflect.Struct:
			before := len(b.fields)
			problems := len(b.problems)
			b.walk(ft, fieldPath, key, fieldIndex)
			if len(b.fields) == before && len(b.problems) == problems {
				b.fail(fmt.Sprintf("field %s: type %v is not supported: a nested struct needs at least one key", fieldPath, ft))
			}
		case supportedValue(ft) || (ft.Kind() == reflect.Slice && supportedValue(ft.Elem())):
			b.fields = append(b.fields, Field{key: key, typ: ft, index: fieldIndex})
			b.paths = append(b.paths, fieldPath)
		default:
			b.fail(fmt.Sprintf("field %s: type %v is not supported", fieldPath, ft))
		}
	}
}

// supportedValue reports whether a key can hold a single value of type t.
func supportedValue(t reflect.Type) bool {
	if t == reflect.TypeFor[time.Duration]() {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// collisions reports every set of fields sharing a matching key, an environment variable or a
// flag, and every key that is also the group of another.
func (b *schemaBuilder) collisions() {
	b.collide("key %q, ignoring case, \"-\" and \"_\",", func(f Field) string { return matchKey(f.key) })
	b.collide("environment variable %q", func(f Field) string { return f.env })
	b.collide("flag %q", func(f Field) string { return f.flag })

	byKey := make(map[string]int, len(b.fields))
	for i, f := range b.fields {
		if _, ok := byKey[matchKey(f.key)]; !ok {
			byKey[matchKey(f.key)] = i
		}
	}
	for i, f := range b.fields {
		segments := strings.Split(matchKey(f.key), ".")
		for n := 1; n < len(segments); n++ {
			if j, ok := byKey[strings.Join(segments[:n], ".")]; ok {
				b.fail(fmt.Sprintf("%s is a value and also the group of %s", b.describe(j), b.describe(i)))
			}
		}
	}
}

// collide reports each value of name shared by more than one field, the fields in declaration
// order and the values in the order of their first field. what is a format naming the value.
func (b *schemaBuilder) collide(what string, name func(Field) string) {
	groups := make(map[string][]int, len(b.fields))
	var order []string
	for i, f := range b.fields {
		n := name(f)
		if _, ok := groups[n]; !ok {
			order = append(order, n)
		}
		groups[n] = append(groups[n], i)
	}
	for _, n := range order {
		members := groups[n]
		if len(members) < 2 {
			continue
		}
		described := make([]string, len(members))
		for k, i := range members {
			described[k] = b.describe(i)
		}
		b.fail(fmt.Sprintf(what+" is shared by %s", n, strings.Join(described, ", ")))
	}
}

// describe names field i by its Go path and its key.
func (b *schemaBuilder) describe(i int) string {
	return fmt.Sprintf("%s (key %q)", b.paths[i], b.fields[i].key)
}
