package config

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// binder sets the entries of every layer on a configuration struct, recording the origin of each
// key and every value that does not convert.
type binder struct {
	schema   *Schema
	target   reflect.Value // the addressable configuration struct
	origins  []string      // origins[i] is the origin of the value of schema.fields[i]
	problems []error
}

func newBinder(schema *Schema, target reflect.Value) *binder {
	origins := make([]string, len(schema.fields))
	for i := range origins {
		origins[i] = originDefault
	}
	return &binder{schema: schema, target: target, origins: origins}
}

// bind converts the value of e to the type of its field and sets it, replacing the value of every
// earlier layer. A value that does not convert leaves the field as it was and is recorded as a
// problem.
func (b *binder) bind(e Entry) {
	i, ok := b.schema.byKey[matchKey(e.Key)]
	if !ok {
		b.problems = append(b.problems, fmt.Errorf("%s: unknown key from %s", e.Key, e.Origin))
		return
	}
	f := b.schema.fields[i]

	var v reflect.Value
	var problems []error
	switch {
	case f.typ.Kind() == reflect.Slice:
		items := e.Items
		if !e.List {
			items = splitList(e.Value)
		}
		v, problems = convertSlice(f, items, e.Origin)
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

// sources returns the effective value and origin of every key, in the order of the Schema's
// fields.
func (b *binder) sources() []Source {
	out := make([]Source, len(b.schema.fields))
	for i, f := range b.schema.fields {
		out[i] = Source{Key: f.key, Value: formatValue(b.target.FieldByIndex(f.index)), Origin: b.origins[i]}
	}
	return out
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

// convertSlice converts each item to the element type of field f and returns the slice, or an
// error for every item that does not convert, numbering the item from 1.
func convertSlice(f Field, items []string, origin string) (reflect.Value, []error) {
	out := reflect.MakeSlice(f.typ, len(items), len(items))
	var problems []error
	for i, item := range items {
		v, err := convertValue(f.typ.Elem(), item)
		if err != nil {
			problems = append(problems, conversionError(f.key, f.typ.Elem(), item, fmt.Sprintf(" (element %d)", i+1), origin, err))
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
