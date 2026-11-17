package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// binder sets the entries of every layer on a configuration struct, recording the origin of each
// value and every value that does not convert.
type binder struct {
	schema   *Schema
	target   reflect.Value // the addressable configuration struct
	origins  []string      // origins[i] is the origin of the value of schema.fields[i], a single value
	trees    []*originTree // trees[i] holds the origins of the value of schema.fields[i], a collection
	problems []error
}

// originTree records where each value within a collection came from, mirroring its shape: the
// entries of a map by map key, the fields of an element by their key in the element Schema, and the
// elements of a list by index. A value with no tree of its own, such as an element field no layer
// wrote, came from where the value holding it came from.
type originTree struct {
	origin  string
	entries map[string]*originTree
	items   []*originTree
}

// entry returns the tree of the map entry or element field key, or a tree holding only t's origin.
func (t *originTree) entry(key string) *originTree {
	if c := t.entries[key]; c != nil {
		return c
	}
	return &originTree{origin: t.origin}
}

// item returns the tree of the list element at index i, or a tree holding only t's origin.
func (t *originTree) item(i int) *originTree {
	if i < len(t.items) && t.items[i] != nil {
		return t.items[i]
	}
	return &originTree{origin: t.origin}
}

func newBinder(schema *Schema, target reflect.Value) *binder {
	origins := make([]string, len(schema.fields))
	trees := make([]*originTree, len(schema.fields))
	for i := range origins {
		origins[i] = originDefault
		trees[i] = &originTree{origin: originDefault}
	}
	return &binder{schema: schema, target: target, origins: origins, trees: trees}
}

// bind converts the value of e to the type of its field and sets it over the value of every earlier
// layer: a single value or a slice of them replaces it, and a collection combines with it as its
// field's merge strategy says. A value that does not convert leaves the field as it was and is
// recorded as a problem.
func (b *binder) bind(e Entry) {
	i, ok := b.schema.byKey[matchKey(e.Key)]
	if !ok {
		if hint, removed := b.schema.Removed(e.Key); removed {
			b.problems = append(b.problems, fmt.Errorf("%s: key was removed: %s from %s", e.Key, hint, e.Origin))
			return
		}
		b.problems = append(b.problems, fmt.Errorf("%s: unknown key from %s", e.Key, e.Origin))
		return
	}
	f := b.schema.fields[i]
	if f.kind != KindValue {
		b.bindCollection(i, e)
		return
	}

	var v reflect.Value
	var problems []error
	switch {
	case e.Node != nil:
		problems = []error{fmt.Errorf("%s: a %v node from %s, but it holds %s", f.key, e.Node.Kind, e.Origin, typeShape(f.typ))}
	case f.typ.Kind() == reflect.Slice:
		items := e.Items
		if !e.List {
			items = splitList(e.Value)
		}
		v, problems = convertSlice(f.key, f.typ, items, e.Origin)
	case e.List:
		problems = []error{fmt.Errorf("%s: a list from %s, but it holds a single %v", f.key, e.Origin, f.typ)}
	default:
		var err error
		v, err = convertValue(f.typ, e.Value)
		if err != nil {
			problems = []error{conversionError(f.key, f.typ, e.Value, "", e.Origin, err)}
		}
	}
	if len(problems) > 0 {
		b.problems = append(b.problems, problems...)
		return
	}
	b.target.FieldByIndex(f.index).Set(v)
	b.origins[i] = e.Origin
}

// bindCollection builds the collection of e, the Entry of schema.fields[i], from its Node and
// combines it with the value of the earlier layers: without a merge strategy it replaces that value
// whole; a map merged by "keys" takes each of its entries over the entry of the same key; and a list
// merged by a key replaces, where it stands, each element whose key it holds, and appends its other
// elements in its own order.
func (b *binder) bindCollection(i int, e Entry) {
	f := b.schema.fields[i]
	if e.Node == nil {
		b.problems = append(b.problems, fmt.Errorf("%s: no node from %s, but it holds %s", f.key, e.Origin, typeShape(f.typ)))
		return
	}
	v, tree, problems := build(f.typ, f.elem, e.Node, []pathStep{keyStep(f.key)})
	if len(problems) == 0 && f.kind == KindList && f.merge != "" {
		problems = mergeKeyProblems(f, v, func(j int) string { return "from " + tree.item(j).origin })
	}
	if len(problems) > 0 {
		b.problems = append(b.problems, problems...)
		return
	}

	field := b.target.FieldByIndex(f.index)
	switch {
	case f.merge == "":
		field.Set(v)
		b.trees[i] = tree
	case f.kind == KindMap:
		b.trees[i] = mergeMap(field, b.trees[i], v, tree)
	default:
		by, _ := elemField(f.elem, f.merge) // NewSchema accepts a merge key only when it names a field
		b.trees[i] = mergeList(field, b.trees[i], v, tree, by)
	}
}

// mergeMap sets the entries of v, whose origins are in tree, on map field, whose origins are in old,
// and returns the origins of the merged map. The merged map itself takes the origin of v.
func mergeMap(field reflect.Value, old *originTree, v reflect.Value, tree *originTree) *originTree {
	if field.IsNil() {
		field.Set(reflect.MakeMapWithSize(field.Type(), v.Len()))
	}
	merged := &originTree{origin: tree.origin, entries: make(map[string]*originTree, field.Len()+v.Len())}
	for it := field.MapRange(); it.Next(); {
		merged.entries[it.Key().String()] = old.entry(it.Key().String())
	}
	for it := v.MapRange(); it.Next(); {
		field.SetMapIndex(it.Key(), it.Value())
		merged.entries[it.Key().String()] = tree.entry(it.Key().String())
	}
	return merged
}

// mergeList merges the elements of v, whose origins are in tree, into list field, whose origins are
// in old, matching elements on the string element field by, and returns the origins of the merged
// list. The merged list itself takes the origin of v.
func mergeList(field reflect.Value, old *originTree, v reflect.Value, tree *originTree, by Field) *originTree {
	key := func(el reflect.Value) string { return el.FieldByIndex(by.index).String() }
	out := field
	if out.IsNil() {
		out = reflect.MakeSlice(field.Type(), 0, v.Len())
	}
	merged := &originTree{origin: tree.origin, items: make([]*originTree, out.Len())}
	at := make(map[string]int, out.Len()+v.Len())
	for j := range out.Len() {
		merged.items[j] = old.item(j)
		at[key(out.Index(j))] = j
	}
	for j := range v.Len() {
		el := v.Index(j)
		if p, ok := at[key(el)]; ok {
			out.Index(p).Set(el)
			merged.items[p] = tree.item(j)
			continue
		}
		at[key(el)] = out.Len()
		out = reflect.Append(out, el)
		merged.items = append(merged.items, tree.item(j))
	}
	field.Set(out)
	return merged
}

// mergeKeyProblems reports each element of v, the list of field f merged by a key, whose merge key
// is empty or holds the value of an earlier element's, naming where the element came from with
// where.
func mergeKeyProblems(f Field, v reflect.Value, where func(int) string) []error {
	by, _ := elemField(f.elem, f.merge) // NewSchema accepts a merge key only when it names a field
	first := make(map[string]int, v.Len())
	var problems []error
	for j := range v.Len() {
		path := formatPath([]pathStep{keyStep(f.key), indexStep(j)})
		key := v.Index(j).FieldByIndex(by.index).String()
		if key == "" {
			problems = append(problems, fmt.Errorf("%s: merge key %q is empty %s; every element of %s needs one", path, f.merge, where(j), f.key))
			continue
		}
		if k, dup := first[key]; dup {
			problems = append(problems, fmt.Errorf("%s: merge key %q is %q %s, as for %s", path, f.merge, key, where(j),
				formatPath([]pathStep{keyStep(f.key), indexStep(k)})))
			continue
		}
		first[key] = j
	}
	return problems
}

// elemField returns the field of element Schema s whose canonical key is key. Keys of an element
// are compared exactly: the Node of an element holds canonical keys.
func elemField(s *Schema, key string) (Field, bool) {
	for _, f := range s.fields {
		if f.key == key {
			return f, true
		}
	}
	return Field{}, false
}

// build converts n, a value read for Go type t at path, to a value of t, with the origin of every
// value within it. elem is the Schema of the struct t holds, at any depth, or nil when it holds
// none. Every problem is reported, naming the path within the layer.
func build(t reflect.Type, elem *Schema, n *Node, path []pathStep) (reflect.Value, *originTree, []error) {
	tree := &originTree{origin: n.Origin}
	at := func(step pathStep) []pathStep { return append(append([]pathStep(nil), path...), step) }
	shape := func() []error {
		return []error{fmt.Errorf("%s: a %v node from %s, but it holds %s", formatPath(path), n.Kind, n.Origin, typeShape(t))}
	}
	missing := func(p []pathStep) error { return fmt.Errorf("%s: no node from %s", formatPath(p), n.Origin) }
	var problems []error

	switch {
	case t.Kind() == reflect.Map:
		if n.Kind != NodeMap {
			return reflect.Value{}, nil, shape()
		}
		out := reflect.MakeMapWithSize(t, len(n.Entries))
		tree.entries = make(map[string]*originTree, len(n.Entries))
		for _, e := range n.Entries {
			p := at(mapKeyStep(e.Key))
			if e.Node == nil {
				problems = append(problems, missing(p))
				continue
			}
			v, child, errs := build(t.Elem(), elem, e.Node, p)
			problems = append(problems, errs...)
			if len(errs) == 0 {
				out.SetMapIndex(reflect.ValueOf(e.Key).Convert(t.Key()), v)
				tree.entries[e.Key] = child
			}
		}
		return out, tree, problems

	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		if n.Kind != NodeList {
			return reflect.Value{}, nil, shape()
		}
		out := reflect.MakeSlice(t, len(n.Items), len(n.Items))
		tree.items = make([]*originTree, len(n.Items))
		for j, item := range n.Items {
			p := at(indexStep(j))
			if item == nil {
				problems = append(problems, missing(p))
				continue
			}
			v, child, errs := build(t.Elem(), elem, item, p)
			problems = append(problems, errs...)
			if len(errs) == 0 {
				out.Index(j).Set(v)
				tree.items[j] = child
			}
		}
		return out, tree, problems

	case t.Kind() == reflect.Struct:
		if n.Kind != NodeMap {
			return reflect.Value{}, nil, shape()
		}
		out := reflect.New(t).Elem()
		tree.entries = make(map[string]*originTree, len(n.Entries))
		for _, e := range n.Entries {
			p := at(keyStep(e.Key))
			f, ok := elemField(elem, e.Key)
			switch {
			case !ok:
				problems = append(problems, fmt.Errorf("%s: unknown key from %s", formatPath(p), n.Origin))
				continue
			case e.Node == nil:
				problems = append(problems, missing(p))
				continue
			}
			v, child, errs := build(f.typ, f.elem, e.Node, p)
			problems = append(problems, errs...)
			if len(errs) == 0 {
				out.FieldByIndex(f.index).Set(v)
				tree.entries[f.key] = child
			}
		}
		return out, tree, problems

	case t.Kind() == reflect.Slice:
		var items []string
		switch n.Kind {
		case NodeScalar:
			items = splitList(n.Value)
		case NodeList:
			items = make([]string, len(n.Items))
			for j, item := range n.Items {
				if item == nil || item.Kind != NodeScalar {
					return reflect.Value{}, nil, shape()
				}
				items[j] = item.Value
			}
		default:
			return reflect.Value{}, nil, shape()
		}
		v, errs := convertSlice(formatPath(path), t, items, n.Origin)
		return v, tree, errs

	default:
		if n.Kind != NodeScalar {
			return reflect.Value{}, nil, shape()
		}
		v, err := convertValue(t, n.Value)
		if err != nil {
			return reflect.Value{}, nil, []error{conversionError(formatPath(path), t, n.Value, "", n.Origin, err)}
		}
		return v, tree, nil
	}
}

// sources returns the effective value and origin of every value, as Result.Sources describes them,
// and an index of the Source of every path within the configuration by its display form: each row,
// and each list, map and element holding rows, the latter with an empty Value.
func (b *binder) sources() ([]Source, map[string]Source) {
	var out []Source
	index := make(map[string]Source)
	for i, f := range b.schema.fields {
		v := b.target.FieldByIndex(f.index)
		if f.kind == KindValue {
			s := Source{Key: f.key, Value: formatValue(v), Origin: b.origins[i]}
			out = append(out, s)
			index[s.Key] = s
			continue
		}
		out = collectSources(out, index, v, f.elem, b.trees[i], []pathStep{keyStep(f.key)})
	}
	return out, index
}

// collectSources appends the rows of v, a value within a collection at path whose origins are in
// tree, to out, and records them and v itself in index. elem is the Schema of the struct v holds,
// at any depth, or nil.
func collectSources(out []Source, index map[string]Source, v reflect.Value, elem *Schema, tree *originTree, path []pathStep) []Source {
	key := formatPath(path)
	at := func(step pathStep) []pathStep { return append(append([]pathStep(nil), path...), step) }
	row := func(value string) []Source {
		s := Source{Key: key, Value: value, Origin: tree.origin}
		index[key] = s
		return append(out, s)
	}
	t := v.Type()
	switch {
	case t.Kind() == reflect.Map:
		if v.Len() == 0 {
			return row("{}")
		}
		index[key] = Source{Key: key, Origin: tree.origin}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
		for _, k := range keys {
			out = collectSources(out, index, v.MapIndex(k), elem, tree.entry(k.String()), at(mapKeyStep(k.String())))
		}
		return out
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		if v.Len() == 0 {
			return row("[]")
		}
		index[key] = Source{Key: key, Origin: tree.origin}
		for j := range v.Len() {
			out = collectSources(out, index, v.Index(j), elem, tree.item(j), at(indexStep(j)))
		}
		return out
	case t.Kind() == reflect.Struct:
		index[key] = Source{Key: key, Origin: tree.origin}
		for _, f := range elem.fields {
			out = collectSources(out, index, v.FieldByIndex(f.index), f.elem, tree.entry(f.key), at(keyStep(f.key)))
		}
		return out
	default:
		return row(formatValue(v))
	}
}

// splitList splits the single text of a slice value on ",", trimming the whitespace around each
// element. The empty text is no elements.
func splitList(text string) []string {
	if text == "" {
		return []string{}
	}
	items := strings.Split(text, ",")
	for i, item := range items {
		items[i] = strings.TrimSpace(item)
	}
	return items
}

// convertSlice converts each item to the element type of slice type t and returns the slice, or an
// error naming key for every item that does not convert, numbering the item from 1.
func convertSlice(key string, t reflect.Type, items []string, origin string) (reflect.Value, []error) {
	out := reflect.MakeSlice(t, len(items), len(items))
	var problems []error
	for i, item := range items {
		v, err := convertValue(t.Elem(), item)
		if err != nil {
			problems = append(problems, conversionError(key, t.Elem(), item, fmt.Sprintf(" (element %d)", i+1), origin, err))
			continue
		}
		out.Index(i).Set(v)
	}
	return out, problems
}

// convertValue converts text to a value of type t, one of the single-value types a Schema accepts.
func convertValue(t reflect.Type, text string) (reflect.Value, error) {
	if t == reflect.TypeFor[time.Duration]() {
		d, err := time.ParseDuration(text)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(d), nil
	}
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.Bool:
		x, err := strconv.ParseBool(text)
		if err != nil {
			return reflect.Value{}, err
		}
		v.SetBool(x)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x, err := strconv.ParseInt(text, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		v.SetInt(x)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		x, err := strconv.ParseUint(text, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		v.SetUint(x)
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(text, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		v.SetFloat(x)
	case reflect.String:
		v.SetString(text)
	default:
		return reflect.Value{}, fmt.Errorf("type %v is not supported", t)
	}
	return v, nil
}

// conversionError reports that text, from origin, is not a value of type t for key. where locates
// an element within a list, and err is the error of the conversion.
func conversionError(key string, t reflect.Type, text, where, origin string, err error) error {
	name := t.Kind().String()
	if t == reflect.TypeFor[time.Duration]() {
		name = "duration"
	}
	msg := fmt.Sprintf("%s: invalid %s %q%s from %s", key, name, text, where, origin)
	if errors.Is(err, strconv.ErrRange) {
		msg += ": out of range"
	}
	return errors.New(msg)
}
