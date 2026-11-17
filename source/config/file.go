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
//
// A collection, a field whose Kind is KindList or KindMap, arrives as one Entry holding the whole
// collection a provider read, as the tree Node; its Value, Items and List are left empty.
type Entry struct {
	// Key is the canonical key of the Schema field the value is for, such as "server.max_retries".
	Key string
	// Value is the raw text of a single value. It is empty when List is true or Node is set.
	Value string
	// Items holds the raw text of each element of a list, in order. It is nil when List is false.
	Items []string
	// List reports whether the value is a list: Items rather than Value.
	List bool
	// Node is the value of a collection field as a tree. It is nil for every other field.
	Node *Node
	// Origin names where the value was read, for error messages, such as "config.yaml:12". For a
	// collection it is the origin of Node.
	Origin string
}

// NodeKind is the shape of a Node.
type NodeKind int

const (
	// NodeScalar is a single value, its text in Value.
	NodeScalar NodeKind = iota
	// NodeList is a list, its elements in Items.
	NodeList
	// NodeMap is a mapping, its entries in Entries.
	NodeMap
)

// String returns "scalar", "list" or "map".
func (k NodeKind) String() string {
	switch k {
	case NodeList:
		return "list"
	case NodeMap:
		return "map"
	default:
		return "scalar"
	}
}

// Node is one value within a collection, as a provider read it, with its origin.
//
// What a Node holds follows the Schema of its collection. A list of structs is a NodeList whose
// Items are its elements. A map is a NodeMap whose Entries hold its keys verbatim, as written, in the
// order written: a map key is never matched, normalised or split on ".". A struct, the element of a
// collection, is a NodeMap whose Entries hold the fields it sets, in the order written, each under
// the canonical key of its field in the element Schema (Field.Elem), dotted for a field of a nested
// struct ("meta.owner"). A field of an element holding a collection is a Node of the same shapes; a
// single value is a NodeScalar; and a slice of single values is a NodeList of NodeScalar items, or a
// NodeScalar holding the text conversion splits, as Entry holds one.
//
// An empty list or mapping, "[]" or "{}", is a NodeList or NodeMap with no Items or Entries; an
// empty element ("- {}") is a NodeMap with no Entries, so it is still one element of its list.
type Node struct {
	// Kind is the shape of the value.
	Kind NodeKind
	// Value is the raw text of a NodeScalar. It is empty for any other kind.
	Value string
	// Items holds the elements of a NodeList, in order: non-nil for a NodeList, nil otherwise.
	Items []*Node
	// Entries holds the entries of a NodeMap, in the order written: non-nil for a NodeMap, nil
	// otherwise. No two entries share a Key.
	Entries []NodeEntry
	// Origin names where the value was read, such as "config.yaml:12".
	Origin string
}

// NodeEntry is one entry of a NodeMap: a map key as written, or the canonical key of an element
// field, and its value.
type NodeEntry struct {
	Key  string
	Node *Node
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
// file holding only comments set nothing. An alias stands for the node it points to, so a value
// reached through one is read as written at its anchor, origin included. A merge key "<<" is not
// interpreted: it is an unknown key.
//
// A collection is read whole into one Entry, as a Node tree with an origin per node. Beneath a map
// any key is accepted, verbatim: it is never matched as keys are, and a "." in it is part of the
// key. Within an element, a struct, keys are matched against the element's Schema as the file's
// keys are against the Schema, nested mappings and dotted keys included, and an unknown key is a
// problem again. An empty element, "- {}", and an empty list or mapping, "[]" or "{}", are read as
// nodes, not skipped.
//
// Load reports every problem in one error: a key with no Schema field, a key the Schema lists as
// removed (reported with its hint, whatever value it holds), a key set twice under any
// two spellings, a mapping or list where the field holds a single value, a value where the key
// names a group of keys, a list element that is not a single value, and an explicit null ("key:"
// with no value, or "~"), which sets nothing and is therefore refused rather than ignored. Within a
// collection the same problems apply, along with a value of another shape than its type holds, a
// map key written twice, and, for a list merged by a key (Field.Merge), an element whose merge key
// is missing or empty or holds the same value as an earlier element's.
//
// Each origin and each problem names the base name of the file and a line, "config.yaml:12": the
// base name keeps messages the same wherever the program runs from. A problem within a collection
// also names the path of the value within the file, its list indexes counted in that file, such as
// profiles[2].env["FOO"]. An error reading or parsing the file names path as given.
func File(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Strict reports true: every key in a file must belong to the Schema.
func (p *FileProvider) Strict() bool { return true }

// Load reads the file and returns an Entry for every key it sets, in the order the file sets them,
// a collection as one Entry holding its Node.
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

	l := &fileLoader{name: filepath.Base(p.path), lines: make(map[*Node]int)}
	l.mapping(newScope(schema, nil, l.setField), root, "")
	if len(l.problems) > 0 {
		return nil, errors.Join(l.problems...)
	}
	return l.entries, nil
}

// fileLoader flattens the mapping of one file into entries, collecting every problem.
type fileLoader struct {
	name     string        // base name of the file, the start of every origin
	lines    map[*Node]int // the line of every Node read, for problems found once it is built
	entries  []Entry
	problems []error
}

// scope is a struct whose keys a mapping sets: the configuration itself, or an element of a
// collection.
type scope struct {
	schema *Schema                         // the Schema keys are looked up in
	groups map[string]struct{}             // matchKey form of every key prefix that is a group of fields
	seen   map[string]seenKey              // matchKey form of every field key set so far
	path   []pathStep                      // the file-local path of the element; nil for the configuration
	set    func(Field, string, *yaml.Node) // records the value of a field, written as the given key
}

func newScope(schema *Schema, path []pathStep, set func(Field, string, *yaml.Node)) *scope {
	return &scope{schema: schema, groups: groupKeys(schema), seen: make(map[string]seenKey), path: path, set: set}
}

// seenKey is where a field key was first set, under the spelling it was written with.
type seenKey struct {
	written string
	line    int
}

func (l *fileLoader) fail(line int, format string, args ...any) {
	l.problems = append(l.problems, fmt.Errorf("%s:%d: %s", l.name, line, fmt.Sprintf(format, args...)))
}

// failIn reports a problem with a key of the element or map at path, which the problem starts with.
// A problem at the top level of the file, where path is nil, names no path.
func (l *fileLoader) failIn(path []pathStep, line int, format string, args ...any) {
	if path != nil {
		format = formatPath(path) + ": " + format
	}
	l.fail(line, format, args...)
}

func (l *fileLoader) origin(n *yaml.Node) string {
	return fmt.Sprintf("%s:%d", l.name, n.Line)
}

// newNode returns the Node of kind read from v, with no items or entries yet, recording its line.
func (l *fileLoader) newNode(kind NodeKind, v *yaml.Node) *Node {
	n := &Node{Kind: kind, Origin: l.origin(v)}
	switch kind {
	case NodeList:
		n.Items = []*Node{}
	case NodeMap:
		n.Entries = []NodeEntry{}
	case NodeScalar:
		n.Value = v.Value
	}
	l.lines[n] = v.Line
	return n
}

// mapping flattens the pairs of mapping n into the fields of s, the keys sitting below the dotted
// key prefix as written.
func (l *fileLoader) mapping(s *scope, n *yaml.Node, prefix string) {
	for i := 0; i+1 < len(n.Content); i += 2 { // [lydite:exclude_from_mutation][the YAML parser gives every mapping node an even number of content nodes, so i+1 == len(n.Content) never holds and < and <= stop at the same i]
		k, v := resolve(n.Content[i]), resolve(n.Content[i+1])
		if k.Kind != yaml.ScalarNode {
			l.failIn(s.path, k.Line, "a key below %q is %s, not a name", prefix, kindName(k))
			continue
		}
		key := k.Value
		if prefix != "" {
			key = prefix + "." + key
		}
		l.pair(s, key, k, v)
	}
}

// pair records in s the value v of key, written as key and at the line of key node k.
func (l *fileLoader) pair(s *scope, key string, k, v *yaml.Node) {
	field, ok := s.schema.Lookup(key)
	if !ok {
		_, group := s.groups[matchKey(key)]
		switch {
		case group && v.Kind == yaml.MappingNode:
			l.mapping(s, v, key)
		case group:
			l.failIn(s.path, k.Line, "key %q names a group of keys, not a value; it takes a mapping, not %s", key, kindName(v))
		default:
			if hint, removed := s.schema.Removed(key); removed {
				l.failIn(s.path, k.Line, "key %q was removed: %s", key, hint)
				return
			}
			l.failIn(s.path, k.Line, "unknown key %q", key)
		}
		return
	}

	m := matchKey(key)
	if first, dup := s.seen[m]; dup {
		l.failIn(s.path, k.Line, "key %q sets the same key as %q at line %d", key, first.written, first.line)
		return
	}
	s.seen[m] = seenKey{written: key, line: k.Line}
	s.set(field, key, v)
}

// setField records the value v of field, a field of the configuration written as key, as an Entry.
func (l *fileLoader) setField(field Field, key string, v *yaml.Node) {
	origin := l.origin(v)
	switch {
	case isNull(v):
		l.fail(v.Line, "key %q is null; give it a value or remove it", key)
	case field.kind != KindValue:
		if n := l.node(field, []pathStep{keyStep(field.key)}, field.typ, v); n != nil {
			l.entries = append(l.entries, Entry{Key: field.key, Node: n, Origin: n.Origin})
		}
	case v.Kind == yaml.ScalarNode:
		l.entries = append(l.entries, Entry{Key: field.key, Value: v.Value, Origin: origin})
	case v.Kind == yaml.SequenceNode && field.typ.Kind() != reflect.Slice:
		l.fail(v.Line, "key %q is a list, but it holds %s", key, typeShape(field.typ))
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
		l.fail(v.Line, "key %q is %s, but it holds %s", key, kindName(v), typeShape(field.typ))
	}
}

// node returns the Node of v, a value of Go type t at the file-local path, within the collection
// field, or nil after reporting every problem in it. t is field's type or a type it holds: a map, a
// slice, the element struct, or a single value or slice of them.
func (l *fileLoader) node(field Field, path []pathStep, t reflect.Type, v *yaml.Node) *Node {
	structs := t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct
	single := t.Kind() != reflect.Map && t.Kind() != reflect.Struct && !structs
	switch {
	case isNull(v):
		l.fail(v.Line, "%s is null; give it a value or remove it", formatPath(path))
		return nil
	case t.Kind() == reflect.Map && v.Kind == yaml.MappingNode:
		return l.mapNode(field, path, t, v)
	case structs && v.Kind == yaml.SequenceNode:
		return l.listNode(field, path, t, v)
	case t.Kind() == reflect.Struct && v.Kind == yaml.MappingNode:
		return l.elementNode(field, path, v)
	case single && v.Kind == yaml.ScalarNode:
		return l.newNode(NodeScalar, v)
	case single && t.Kind() == reflect.Slice && v.Kind == yaml.SequenceNode:
		return l.scalarListNode(path, v)
	default:
		l.fail(v.Line, "%s is %s, but it holds %s", formatPath(path), kindName(v), typeShape(t))
		return nil
	}
}

// mapNode returns the NodeMap of mapping v, a map of type t at path, its keys taken verbatim.
func (l *fileLoader) mapNode(field Field, path []pathStep, t reflect.Type, v *yaml.Node) *Node {
	n := l.newNode(NodeMap, v)
	lines := make(map[string]int)
	valid := true
	for i := 0; i+1 < len(v.Content); i += 2 { // [lydite:exclude_from_mutation][the YAML parser gives every mapping node an even number of content nodes, so i+1 == len(v.Content) never holds and < and <= stop at the same i]
		k, val := resolve(v.Content[i]), resolve(v.Content[i+1])
		if k.Kind != yaml.ScalarNode {
			l.failIn(path, k.Line, "a key is %s, not a name", kindName(k))
			valid = false
			continue
		}
		if first, dup := lines[k.Value]; dup {
			l.failIn(path, k.Line, "key %q is set again; it is first set at line %d", k.Value, first)
			valid = false
			continue
		}
		lines[k.Value] = k.Line
		child := l.node(field, append(append([]pathStep(nil), path...), mapKeyStep(k.Value)), t.Elem(), val)
		if child == nil {
			valid = false
			continue
		}
		n.Entries = append(n.Entries, NodeEntry{Key: k.Value, Node: child})
	}
	if !valid {
		return nil
	}
	return n
}

// listNode returns the NodeList of sequence v, a slice of structs of type t at path, and checks the
// merge key of each element when field is merged by one. Only field itself is such a slice: the
// field of an element is passed as its own field, and a map holds no slice of structs.
func (l *fileLoader) listNode(field Field, path []pathStep, t reflect.Type, v *yaml.Node) *Node {
	n := l.newNode(NodeList, v)
	valid := true
	for i, item := range v.Content {
		child := l.node(field, append(append([]pathStep(nil), path...), indexStep(i)), t.Elem(), resolve(item))
		if child == nil {
			valid = false
			continue
		}
		n.Items = append(n.Items, child)
	}
	if !valid {
		return nil
	}
	if field.merge != "" && !l.mergeKeys(field, path, n) {
		return nil
	}
	return n
}

// mergeKeys reports whether every element of n, the list of field at path, sets the element field
// field is merged by, to a value no other element holds, reporting each element that does not.
func (l *fileLoader) mergeKeys(field Field, path []pathStep, n *Node) bool {
	type firstKey struct {
		index int
		line  int
	}
	seen := make(map[string]firstKey)
	valid := true
	for i, item := range n.Items {
		elemPath := append(append([]pathStep(nil), path...), indexStep(i))
		var value *Node
		for _, e := range item.Entries {
			if e.Key == field.merge {
				value = e.Node
			}
		}
		if value == nil || value.Value == "" {
			at := item
			if value != nil {
				at = value
			}
			l.failIn(elemPath, l.lines[at], "merge key %q is empty; every element of %s needs one", field.merge, formatPath(path))
			valid = false
			continue
		}
		line := l.lines[value]
		if first, dup := seen[value.Value]; dup {
			l.failIn(elemPath, line, "merge key %q is %q, as for %s at line %d", field.merge, value.Value,
				formatPath(append(append([]pathStep(nil), path...), indexStep(first.index))), first.line)
			valid = false
			continue
		}
		seen[value.Value] = firstKey{index: i, line: line}
	}
	return valid
}

// elementNode returns the NodeMap of mapping v, an element of field at path, its entries keyed by
// the canonical key of each field of the element Schema it sets. The element is strict as the
// configuration is: a key of no element field is a problem.
func (l *fileLoader) elementNode(field Field, path []pathStep, v *yaml.Node) *Node {
	n := l.newNode(NodeMap, v)
	before := len(l.problems)
	s := newScope(field.elem, path, func(f Field, _ string, val *yaml.Node) {
		if child := l.node(f, append(append([]pathStep(nil), path...), keyStep(f.key)), f.typ, val); child != nil {
			n.Entries = append(n.Entries, NodeEntry{Key: f.key, Node: child})
		}
	})
	l.mapping(s, v, "")
	if len(l.problems) > before {
		return nil
	}
	return n
}

// scalarListNode returns the NodeList of sequence v, a slice of single values at path, each element
// a NodeScalar.
func (l *fileLoader) scalarListNode(path []pathStep, v *yaml.Node) *Node {
	n := l.newNode(NodeList, v)
	valid := true
	for i, item := range v.Content {
		item = resolve(item)
		subject := formatPath(append(append([]pathStep(nil), path...), indexStep(i)))
		switch {
		case isNull(item):
			l.fail(item.Line, "%s is null; give it a value or remove it", subject)
			valid = false
		case item.Kind != yaml.ScalarNode:
			l.fail(item.Line, "%s is %s, not a single value", subject, kindName(item))
			valid = false
		default:
			n.Items = append(n.Items, l.newNode(NodeScalar, item))
		}
	}
	if !valid {
		return nil
	}
	return n
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

// typeShape describes what a value of Go type t holds for an error message: "a single int", "a
// list of string", "a mapping of string" for a map, "the keys of config.profile" for a struct.
func typeShape(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Slice:
		return fmt.Sprintf("a list of %v", t.Elem())
	case reflect.Map:
		return fmt.Sprintf("a mapping of %v", t.Elem())
	case reflect.Struct:
		return fmt.Sprintf("the keys of %v", t)
	default:
		return fmt.Sprintf("a single %v", t)
	}
}
