package config

import (
	"errors"
	"fmt"
	"slices"
)

// FlagsProvider reads the values of command-line flags. Flags returns one.
type FlagsProvider struct {
	values map[string]string
}

// Flags returns a provider for the flags a command line set explicitly. values maps the key of each
// flag that was set to its text; a flag left at its default is absent, so that it does not override
// a lower layer. The package parses no command line itself.
//
// A key matches its Schema field ignoring case, "-" and "_" within each segment, so "max-retries"
// finds "max_retries" and "Server.Max-Retries" finds "server.max_retries". A key of slice type
// arrives as the single comma-separated text of its flag; it is not split here.
//
// Load reports every problem in one error, the keys in sorted order: a key with no Schema field,
// a key the Schema lists as removed, reported with its hint, and two keys that match the same field.
// Origins name the flag by its canonical name, such as "flag --server.max-retries", however the key
// was spelled.
func Flags(values map[string]string) *FlagsProvider {
	return &FlagsProvider{values: values}
}

// Strict reports true: every flag set must belong to the Schema.
func (p *FlagsProvider) Strict() bool { return true }

// Load returns an Entry for every key in the map, in the order of the Schema's fields.
func (p *FlagsProvider) Load(schema *Schema) ([]Entry, error) {
	keys := make([]string, 0, len(p.values))
	for k := range p.values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var problems []error
	written := make(map[string]string, len(keys)) // canonical key to the first spelling that matched it
	for _, k := range keys {
		f, ok := schema.Lookup(k)
		if !ok {
			problems = append(problems, fmt.Errorf("flag --%s: unknown key", k))
			continue
		}
		if first, dup := written[f.key]; dup {
			problems = append(problems, fmt.Errorf("flag --%s sets the same key as --%s", k, first))
			continue
		}
		written[f.key] = k
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}

	var entries []Entry
	for _, f := range schema.fields {
		k, ok := written[f.key]
		if !ok {
			continue
		}
		entries = append(entries, Entry{Key: f.key, Value: p.values[k], Origin: "flag --" + f.flag})
	}
	return entries, nil
}
