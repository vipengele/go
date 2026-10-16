package config

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
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
//
// A `doc:"..."` tag describes a key, and is returned by [Field.Doc]; on a field of struct type it
// describes the group, and is returned by [Schema.GroupDoc]. A `doc` tag on a field with no key, one
// tagged `config:"-"` or unexported, is a schema error, since nothing would ever show it.
//
// When the struct, or a pointer to it, has a method RemovedKeys() map[string]string, each entry is a
// key the struct no longer accepts, mapped to a hint telling the user what to write instead; see
// [Schema.Removed]. Only the top-level struct is asked, never a nested one.
type Schema struct {
	fields    []Field
	byKey     map[string]int
	groupDocs map[string]string // keyed by the matchKey of the group's key
	removed   map[string]string // keyed by the matchKey of the removed key
}

// Field is one key of a Schema.
type Field struct {
	key   string
	env   string
	flag  string
	doc   string
	typ   reflect.Type
	index []int
}

// removedKeyer is the method a configuration struct implements to name the keys it no longer
// accepts, each mapped to a migration hint.
type removedKeyer interface {
	RemovedKeys() map[string]string
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

// Doc returns the `doc:"..."` tag of the struct field the key binds to, or "" without one.
func (f Field) Doc() string { return f.doc }

// NewSchema reflects the Schema of target, a struct or a pointer to a struct, whose environment
// variables start with envPrefix. envPrefix is empty, or an upper-case ASCII letter followed by
// upper-case letters, digits and underscores, not ending in an underscore.
//
// Every problem is reported in one error: each unsupported field, and each set of fields that
// collide. Fields collide when their keys match, ignoring case, "-" and "_"; when they derive the
// same environment variable or flag; or when one key is the group of another ("server" and
// "server.port"), which a nested source such as a file cannot hold at once. A `doc` tag on a field
// with no key is a problem, and so is each RemovedKeys entry that is not a valid dotted key, that
// matches a key or the group of a key, or that matches another entry, matching as Lookup does.
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
	var removed map[string]string
	if r, ok := reflect.New(t).Interface().(removedKeyer); ok {
		removed = b.removedKeys(root, r.RemovedKeys())
	}
	if len(b.problems) > 0 {
		return nil, errors.Join(b.problems...)
	}

	s := &Schema{
		fields:    b.fields,
		byKey:     make(map[string]int, len(b.fields)),
		groupDocs: make(map[string]string, len(b.groupDocs)),
		removed:   removed,
	}
	for i, f := range b.fields {
		s.byKey[matchKey(f.key)] = i
	}
	for key, doc := range b.groupDocs {
		s.groupDocs[matchKey(key)] = doc
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

// GroupDoc returns the `doc:"..."` tag of the nested struct field whose group key matches key, as
// Lookup matches: GroupDoc("rate-limit") is the doc of the field holding the "rate_limit.*" keys.
// It returns "" for a group without a doc tag and for a key that names no group.
func (s *Schema) GroupDoc(key string) string {
	return s.groupDocs[matchKey(key)]
}

// Removed reports whether key matches, as Lookup matches, a key the configuration struct names in
// its RemovedKeys method, and returns the hint the method maps it to.
func (s *Schema) Removed(key string) (hint string, ok bool) {
	hint, ok = s.removed[matchKey(key)]
	return hint, ok
}

// schemaBuilder collects the fields of a struct and every problem found while reflecting it.
type schemaBuilder struct {
	fields    []Field
	paths     []string          // paths[i] is the Go path of fields[i], such as Config.Server.Port.
	groupDocs map[string]string // the doc tag of each nested struct field, by its canonical key
	problems  []error
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
		doc, documented := sf.Tag.Lookup("doc")
		fieldPath := path + "." + sf.Name
		if tag == "-" {
			if documented {
				b.fail(fmt.Sprintf("field %s: a doc tag on a field tagged config:\"-\" describes no key", fieldPath))
			}
			continue
		}
		if sf.Anonymous {
			b.fail(fmt.Sprintf("field %s: embedded fields are not supported; give the field a name", fieldPath))
			continue
		}
		if !sf.IsExported() {
			if documented {
				b.fail(fmt.Sprintf("field %s: a doc tag on an unexported field describes no key", fieldPath))
			}
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
			if doc != "" {
				if b.groupDocs == nil {
					b.groupDocs = make(map[string]string)
				}
				b.groupDocs[key] = doc
			}
		case supportedValue(ft) || (ft.Kind() == reflect.Slice && supportedValue(ft.Elem())):
			b.fields = append(b.fields, Field{key: key, doc: doc, typ: ft, index: fieldIndex})
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

// removedKeys validates the entries of the RemovedKeys method of root and returns them keyed by
// matchKey. An entry is a problem when a segment of it is not a valid key segment, or when it
// matches a key or the group of a key; entries matching each other are one problem naming them all.
// Entries are checked in sorted order, so that the problems are reported in the same order on every
// run.
func (b *schemaBuilder) removedKeys(root string, entries map[string]string) map[string]string {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	live := make(map[string]int, len(b.fields))
	groups := make(map[string]int, len(b.fields))
	for i, f := range b.fields {
		m := matchKey(f.key)
		if _, ok := live[m]; !ok {
			live[m] = i
		}
		segments := strings.Split(m, ".")
		for n := 1; n < len(segments); n++ { // [lydite:exclude_from_mutation][with <= the whole of m also enters groups, but m is in live, which is checked first, so groups is never read at a key that is also live]
			g := strings.Join(segments[:n], ".")
			if _, ok := groups[g]; !ok {
				groups[g] = i
			}
		}
	}

	matching := make(map[string][]string, len(keys))
	var order []string
	for _, k := range keys {
		what := fmt.Sprintf("removed key %q of %s.RemovedKeys", k, root)
		if !validDottedKey(k) {
			b.fail(fmt.Sprintf("%s is not a dotted key: each \".\"-separated segment must start with an ASCII letter and hold only ASCII letters, digits, \"-\" and \"_\"", what))
			continue
		}
		m := matchKey(k)
		if i, ok := live[m]; ok {
			b.fail(fmt.Sprintf("%s matches the key of %s", what, b.describe(i)))
			continue
		}
		if i, ok := groups[m]; ok {
			b.fail(fmt.Sprintf("%s matches the group of %s", what, b.describe(i)))
			continue
		}
		if _, ok := matching[m]; !ok {
			order = append(order, m)
		}
		matching[m] = append(matching[m], k)
	}

	removed := make(map[string]string, len(order))
	for _, m := range order {
		if ks := matching[m]; len(ks) > 1 {
			quoted := make([]string, len(ks))
			for i, k := range ks {
				quoted[i] = fmt.Sprintf("%q", k)
			}
			b.fail(fmt.Sprintf("removed keys %s of %s.RemovedKeys match each other, ignoring case, \"-\" and \"_\"", strings.Join(quoted, ", "), root))
			continue
		}
		removed[m] = entries[matching[m][0]]
	}
	return removed
}
