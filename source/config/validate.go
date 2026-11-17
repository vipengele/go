package config

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Validator is implemented by a configuration struct that checks its own bound value. Load calls
// Validate on a pointer to the value it bound, so a value receiver and a pointer receiver both
// work, and only when every provider loaded and every value converted. Only the top-level struct is
// asked, never a nested one.
//
// Validate must not change the value: Load returns it as Validate leaves it, with the Sources
// recorded before Validate ran. A problem with one key is reported as a *FieldError naming the
// key, alone or within the error Validate returns, wrapped or joined; Load reports a copy of it
// carrying the key's value and origin.
type Validator interface {
	Validate() error
}

// FieldError is a problem a Validator found with the value of one key.
//
// A Validator sets Key and Err. Load resolves Key as a key, matched as Schema.Lookup matches it, or
// as a path within a collection, such as profiles[2].env["FOO"], its key segments matched the same
// way, its indexes counted in the merged collection and its map keys exact. It reports a copy of the
// FieldError with Key replaced by the canonical key or path and Value and Origin set from what Key
// names: its Source, or for a list, a map or an element holding several values, its origin alone. A
// Key that names nothing is reported as the Validator wrote it.
type FieldError struct {
	// Key is the key or path the problem is about, such as "server.port" or "profiles[2].env".
	Key string
	// Value is the bound value of the key, as Source.Value writes it, and empty for a value that
	// holds several.
	Value string
	// Origin is where the value came from, as Source.Origin names it, such as "config.yaml:12".
	Origin string
	// Err describes the problem, such as "must be below 1024".
	Err error
}

// Error returns the problem in the form of the other errors of Load, the key first and the origin
// last: `server.port: must be below 1024: "8080" from config.yaml:3`. Without an origin the value
// follows the problem alone, and without either the message is the key and the problem.
func (e *FieldError) Error() string {
	msg := "invalid value"
	if e.Err != nil {
		msg = e.Err.Error()
	}
	if e.Key != "" {
		msg = e.Key + ": " + msg
	}
	switch {
	case e.Origin != "":
		msg += fmt.Sprintf(": %q from %s", e.Value, e.Origin)
	case e.Value != "":
		msg += fmt.Sprintf(": %q", e.Value)
	}
	return msg
}

// Unwrap returns Err.
func (e *FieldError) Unwrap() error { return e.Err }

// validationError is the error of a Validate call whose tree holds a *FieldError Load annotated.
// Its text is the text of the error Validate returned with the text of each such FieldError
// replaced by the text of its annotated copy, so a wrapping message such as fmt.Errorf's keeps its
// context. Unwrap returns the annotated copies first, so that errors.As finds one before the
// FieldError it was copied from, and then the error Validate returned, so that errors.Is and
// errors.As still reach everything in it, the caller's own *FieldError values included.
type validationError struct {
	msg  string
	errs []error
}

func (e *validationError) Error() string { return e.msg }

func (e *validationError) Unwrap() []error { return e.errs }

// annotate returns err, the error Validate returned, with every *FieldError in its tree whose key
// resolves, as resolveKey resolves it, reported at its canonical key or path and with the value and
// origin index holds for it. The FieldErrors of err are never changed. An err with no FieldError to
// annotate is returned as it is.
func annotate(err error, schema *Schema, index map[string]Source) error {
	var copies []error
	var pairs [][2]string
	for _, fe := range fieldErrors(err) {
		s, ok := resolveKey(schema, index, fe.Key)
		if !ok {
			continue
		}
		c := &FieldError{Key: s.Key, Value: s.Value, Origin: s.Origin, Err: fe.Err}
		copies = append(copies, c)
		pairs = append(pairs, [2]string{fe.Error(), c.Error()})
	}
	if len(copies) == 0 {
		return err
	}
	// A longer text is replaced before a shorter one it contains, such as "server.port: bad" before
	// "port: bad", and the replacer never matches inside a replacement.
	slices.SortStableFunc(pairs, func(a, b [2]string) int { return cmp.Compare(len(b[0]), len(a[0])) })
	oldnew := make([]string, 0, 2*len(pairs)) // [lydite:exclude_from_mutation][the capacity only reserves space; append grows the slice to the same strings without it]
	for _, p := range pairs {
		oldnew = append(oldnew, p[0], p[1])
	}
	msg := strings.NewReplacer(oldnew...).Replace(err.Error())
	return &validationError{msg: msg, errs: append(copies, err)}
}

// resolveKey returns the Source index holds for key: a key of schema, as Lookup matches it, or a
// path within a collection whose key segments match a key of schema and then of each element Schema
// it enters, as Lookup matches them. Its indexes and map keys are taken as written, and a map key is
// never matched as a key is.
func resolveKey(schema *Schema, index map[string]Source, key string) (Source, bool) {
	if f, ok := schema.Lookup(key); ok {
		s, found := index[f.key]
		return s, found
	}
	steps, err := parsePath(key)
	if err != nil {
		return Source{}, false
	}
	s := schema
	for i := range steps {
		if steps[i].kind != stepKey {
			continue
		}
		if s == nil {
			return Source{}, false
		}
		f, ok := s.Lookup(steps[i].key)
		if !ok {
			return Source{}, false
		}
		steps[i].key, s = f.key, f.elem
	}
	src, ok := index[formatPath(steps)]
	return src, ok
}

// fieldErrors returns every non-nil *FieldError in the tree of err, depth first through Unwrap()
// error and Unwrap() []error, as errors.As walks it. The tree below a FieldError is not searched.
func fieldErrors(err error) []*FieldError {
	var out []*FieldError
	var walk func(error)
	walk = func(err error) {
		if err == nil {
			return
		}
		if fe, ok := err.(*FieldError); ok {
			if fe != nil {
				out = append(out, fe)
			}
			return
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, e := range u.Unwrap() {
				walk(e)
			}
		}
	}
	walk(err)
	return out
}
