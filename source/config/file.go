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

	"go.yaml.in/yaml/v3"
)

// Entry is one value a provider found for a key of a Schema.
//
// A value is kept as the text the user wrote, so that every provider hands conversion the same
// string for the same setting. A single value is Value, with List false. A list is Items, with List
// true, one element per item, each element's text as written: a YAML sequence is a list, and so is
// nothing else a provider reads. A key whose Go type is a slice may still arrive as a single Value,
// such as "a,b" from an environment variable; splitting it is left to conversion, which sees the
// same text from every provider.
type Entry struct {
	// Key is the canonical key of the Schema field the value is for, such as "server.max_retries".
	Key string
	// Value is the raw text of a single value. It is empty when List is true.
	Value string
	// Items holds the raw text of each element of a list, in order. It is nil when List is false.
	Items []string
	// List reports whether the value is a list: Items rather than Value.
	List bool
	// Origin names where the value was read, for error messages, such as "config.yaml:12".
	Origin string
}

// FileProvider reads a YAML configuration file. File returns one.
type FileProvider struct {
	path string
}

// File returns a provider reading the YAML file at path.
//
// The file is a mapping of keys. Nested mappings and dotted keys share one key space, so
// "server.port: 80" and "server:" with "port: 80" below it set the same key, and keys match their
// Schema field ignoring case, "-" and "_" within each segment. A missing file, an empty file and a
// file holding only comments set nothing.
//
// Load reports every problem in one error: a key with no Schema field, a key set twice under any
// two spellings, a mapping or list where the field holds a single value, a value where the key
// names a group of keys, a list element that is not a single value, and an explicit null ("key:"
// with no value, or "~"), which sets nothing and is therefore refused rather than ignored.
//
// Each origin and each problem names the base name of the file and a line, "config.yaml:12": the
// base name keeps messages the same wherever the program runs from. An error reading or parsing the
// file names path as given.
func File(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Strict reports true: every key in a file must belong to the Schema.
func (p *FileProvider) Strict() bool { return true }

// Load reads the file and returns an Entry for every key it sets, in the order the file sets them.
func (p *FileProvider) Load(schema *Schema) ([]Entry, error) {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config file %s: %w", p.path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("config file %s: %w", p.path, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("config file %s: %w", p.path, err)
		}
		return nil, fmt.Errorf("config file %s: line %d: a second YAML document; the file holds one", p.path, extra.Line)
	}

	root := &doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, nil
		}
		root = resolve(root.Content[0])
	}
	switch {
	case root.Kind == 0, isNull(root):
		return nil, nil
	case root.Kind != yaml.MappingNode:
		return nil, fmt.Errorf("config file %s: line %d: the top level is %s, not a mapping of keys", p.path, root.Line, kindName(root))
	}

	l := &fileLoader{
		name:   filepath.Base(p.path),
		schema: schema,
		groups: groupKeys(schema),
		seen:   make(map[string]seenKey),
	}
	l.mapping(root, "")
	if len(l.problems) > 0 {
		return nil, errors.Join(l.problems...)
	}
	return l.entries, nil
}

// fileLoader flattens the mapping of one file into entries, collecting every problem.
type fileLoader struct {
	name     string              // base name of the file, the start of every origin
	schema   *Schema             // the Schema keys are looked up in
	groups   map[string]struct{} // matchKey form of every key prefix that is a group of fields
	seen     map[string]seenKey  // matchKey form of every field key set so far
	entries  []Entry
	problems []error
}

// seenKey is where a field key was first set, under the spelling it was written with.
type seenKey struct {
	written string
	line    int
}

func (l *fileLoader) fail(line int, format string, args ...any) {
	l.problems = append(l.problems, fmt.Errorf("%s:%d: %s", l.name, line, fmt.Sprintf(format, args...)))
}

// mapping flattens the pairs of mapping n, whose keys sit below the dotted key prefix as written.
func (l *fileLoader) mapping(n *yaml.Node, prefix string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := resolve(n.Content[i]), resolve(n.Content[i+1])
		if k.Kind != yaml.ScalarNode {
			l.fail(k.Line, "a key below %q is %s, not a name", prefix, kindName(k))
			continue
		}
		key := k.Value
		if prefix != "" {
			key = prefix + "." + key
		}
		l.pair(key, k, v)
	}
}

// pair records the value v of key, written as key and at the line of key node k.
func (l *fileLoader) pair(key string, k, v *yaml.Node) {
	field, ok := l.schema.Lookup(key)
	if !ok {
		_, group := l.groups[matchKey(key)]
		switch {
		case group && v.Kind == yaml.MappingNode:
			l.mapping(v, key)
		case group:
			l.fail(k.Line, "key %q names a group of keys, not a value; it takes a mapping, not %s", key, kindName(v))
		default:
			l.fail(k.Line, "unknown key %q", key)
		}
		return
	}

	m := matchKey(key)
	if first, dup := l.seen[m]; dup {
		l.fail(k.Line, "key %q sets the same key as %q at line %d", key, first.written, first.line)
		return
	}
	l.seen[m] = seenKey{written: key, line: k.Line}

	origin := fmt.Sprintf("%s:%d", l.name, v.Line)
	switch {
	case isNull(v):
		l.fail(v.Line, "key %q is null; give it a value or remove it", key)
	case v.Kind == yaml.ScalarNode:
		l.entries = append(l.entries, Entry{Key: field.key, Value: v.Value, Origin: origin})
	case v.Kind == yaml.SequenceNode && field.typ.Kind() != reflect.Slice:
		l.fail(v.Line, "key %q is a list, but it holds %s", key, fieldShape(field))
	case v.Kind == yaml.SequenceNode:
		items := make([]string, 0, len(v.Content))
		valid := true
		for _, item := range v.Content {
			item = resolve(item)
			switch {
			case isNull(item):
				l.fail(item.Line, "an element of key %q is null; give it a value or remove it", key)
				valid = false
			case item.Kind != yaml.ScalarNode:
				l.fail(item.Line, "an element of key %q is %s, not a single value", key, kindName(item))
				valid = false
			default:
				items = append(items, item.Value)
			}
		}
		if valid {
			l.entries = append(l.entries, Entry{Key: field.key, Items: items, List: true, Origin: origin})
		}
	default:
		l.fail(v.Line, "key %q is %s, but it holds %s", key, kindName(v), fieldShape(field))
	}
}

// groupKeys returns the matchKey form of every proper prefix of a key of s: the groups a file may
// open as a mapping.
func groupKeys(s *Schema) map[string]struct{} {
	groups := make(map[string]struct{})
	for _, f := range s.fields {
		m := matchKey(f.key)
		for i := range len(m) {
			if m[i] == '.' {
				groups[m[:i]] = struct{}{}
			}
		}
	}
	return groups
}

// resolve returns the node an alias points to, and any other node itself.
func resolve(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// isNull reports whether n is an explicit YAML null: "~", "null" or no value at all.
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// kindName describes the shape of n for an error message.
func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.ScalarNode:
		return fmt.Sprintf("the value %q", n.Value)
	default:
		return "an unsupported YAML node"
	}
}

// fieldShape describes what field holds for an error message: "a single int", "a list of string".
func fieldShape(field Field) string {
	if field.typ.Kind() == reflect.Slice {
		return fmt.Sprintf("a list of %v", field.typ.Elem())
	}
	return fmt.Sprintf("a single %v", field.typ)
}
