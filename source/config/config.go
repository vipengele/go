package config

import (
	"errors"
	"fmt"
	"reflect"
)

// Provider is a layer of configuration: a source of values for the keys of a Schema. File, Env and
// Flags return the providers of the package; any other type with these methods is one too.
type Provider interface {
	// Load returns an Entry for every key the provider sets, each naming a key of schema. An error
	// reports every problem the provider found.
	Load(schema *Schema) ([]Entry, error)
	// Strict reports whether the provider refuses a key unknown to the Schema. A strict provider
	// reports such a key itself, as an error from Load; a provider that is not strict reads only
	// the keys of the Schema and ignores everything else in its source.
	Strict() bool
}

var (
	_ Provider = (*FileProvider)(nil)
	_ Provider = (*EnvProvider)(nil)
	_ Provider = (*FlagsProvider)(nil)
)

// originDefault is the origin of a key no provider sets.
const originDefault = "default"

// Result is the configuration Load bound, with where each of its values came from.
type Result[T any] struct {
	value   T
	sources []Source
}

// Value returns the bound configuration: defaults overridden by every provider, in order.
func (r Result[T]) Value() T { return r.value }

// Sources returns one Source for every key of the configuration, in the order of the Schema's
// fields. The slice is a copy; changing it does not change the Result.
func (r Result[T]) Sources() []Source {
	out := make([]Source, len(r.sources))
	copy(out, r.sources)
	return out
}

// Load binds the configuration of struct type T from defaults and providers.
//
// The first layer is defaults itself: every key starts at the value of its field in defaults, with
// origin "default". Each provider is a layer above the previous one, in the order given, and a key
// it sets replaces the value of every layer below, a slice included: the elements of two layers are
// never merged. The conventional order is File, Env, Flags, so that a flag overrides an
// environment variable, which overrides a file, which overrides the defaults.
//
// T is a struct, not a pointer, and its Schema is reflected as NewSchema describes; an invalid
// Schema is returned as NewSchema reports it, matching ErrSchema. A value converts to the type of
// its field as follows: a bool through strconv.ParseBool; an integer or a float through strconv
// at the size of the field, a value out of its range being an error; a time.Duration through
// time.ParseDuration; a string as written. A slice takes each item of a list as one element, and
// splits a single value, such as an environment variable, on ",", trimming the whitespace around
// each element; an empty single value is an empty slice.
//
// Every problem is reported in one error, in layer order: first the error of each provider's Load,
// then each value that does not convert, naming its key, its text and its origin, such as
// `server.max_retries: invalid int "abc" from env APP_SERVER_MAX_RETRIES`. A provider whose Load
// fails sets nothing, and the providers after it are still loaded so that their problems are
// reported too. A nil provider is an error.
//
// When every value is bound and T, or a pointer to T, implements Validator, Load calls Validate on
// the bound value, and an error it returns is the error of Load. Each *FieldError in that error's
// tree, reached through Unwrap() error and Unwrap() []error, whose Key matches a key as
// Schema.Lookup matches is reported with the value and origin of its key, such as
// `server.port: must be below 1024: "8080" from config.yaml:3`, and errors.As finds that copy
// before the FieldError Validate returned, which Load leaves unchanged. Every other error in the
// tree is reported as Validate returned it.
//
// On any problem Load returns the zero Result.
func Load[T any](defaults T, providers ...Provider) (Result[T], error) {
	if t := reflect.TypeFor[T](); t.Kind() != reflect.Struct {
		return Result[T]{}, fmt.Errorf("%w: configuration type is %v, not a struct", ErrSchema, t)
	}
	// The Schema's environment prefix names no variable Load reads: Env derives every name from its
	// own prefix, and a prefix shared by every name makes no two of them collide that did not
	// collide without it.
	schema, err := NewSchema(defaults, "")
	if err != nil {
		return Result[T]{}, err
	}

	var problems []error
	layers := make([][]Entry, 0, len(providers))
	for i, p := range providers {
		if isNil(p) {
			problems = append(problems, fmt.Errorf("config: provider %d is nil", i))
			continue
		}
		entries, err := p.Load(schema)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		layers = append(layers, entries)
	}

	value := defaults
	target := reflect.ValueOf(&value).Elem()
	cloneSlices(schema, target)
	b := newBinder(schema, target)
	for _, entries := range layers {
		for _, e := range entries {
			b.bind(e)
		}
	}
	problems = append(problems, b.problems...)
	if len(problems) > 0 {
		return Result[T]{}, errors.Join(problems...)
	}
	sources := b.sources()
	if v, ok := any(&value).(Validator); ok {
		if err := v.Validate(); err != nil {
			return Result[T]{}, annotate(err, schema, sources)
		}
	}
	return Result[T]{value: value, sources: sources}, nil
}

// cloneSlices gives every slice field of schema in v a backing array of its own, holding the same
// elements. v starts as a copy of the caller's defaults, and a copied slice shares its array: a key
// no provider sets would otherwise let a change to an element of the Result's value change
// defaults, and the other way round. A nil slice stays nil, so that nil and empty stay distinct.
func cloneSlices(schema *Schema, v reflect.Value) {
	for _, f := range schema.fields {
		if f.typ.Kind() != reflect.Slice {
			continue
		}
		field := v.FieldByIndex(f.index)
		if field.IsNil() {
			continue
		}
		clone := reflect.MakeSlice(f.typ, field.Len(), field.Len())
		reflect.Copy(clone, field)
		field.Set(clone)
	}
}

// isNil reports whether p is nil or holds a nil pointer, on which Load cannot be called.
func isNil(p Provider) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
