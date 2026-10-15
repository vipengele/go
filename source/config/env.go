package config

import (
	"errors"
	"fmt"
)

// EnvProvider reads configuration from environment variables. Env returns one.
type EnvProvider struct {
	prefix string
	lookup func(string) (string, bool)
}

// Env returns a provider reading the environment variable of each key of the Schema, found through
// lookup, which has the signature of os.LookupEnv. The package never reads the process environment
// itself: the caller passes os.LookupEnv, or a fake.
//
// The variable of a key is derived from prefix, not from the prefix the Schema was built with: the
// key upper-cased with "." replaced by "_", after prefix and an underscore when prefix is not
// empty, so "server.max_retries" under "APP" is APP_SERVER_MAX_RETRIES. Only these names are
// looked up, so other variables that start with prefix are ignored.
//
// A variable that is set, even to the empty string, is a value. A key of slice type arrives as the
// single comma-separated text of its variable; it is not split here.
//
// Load reports an error for a nil lookup and for a prefix that is not empty, or an upper-case ASCII
// letter followed by upper-case letters, digits and underscores, not ending in an underscore.
func Env(prefix string, lookup func(string) (string, bool)) *EnvProvider {
	return &EnvProvider{prefix: prefix, lookup: lookup}
}

// Strict reports false: variables outside the Schema are not the provider's to judge, since the
// environment is shared with other programs.
func (p *EnvProvider) Strict() bool { return false }

// Load looks up the variable of every key of the Schema and returns an Entry for each one that is
// set, in the order of the Schema's fields.
func (p *EnvProvider) Load(schema *Schema) ([]Entry, error) {
	if p.lookup == nil {
		return nil, errors.New("config env: lookup is nil")
	}
	if !validPrefix(p.prefix) {
		return nil, fmt.Errorf("config env: prefix %q is not an upper-case ASCII letter followed by upper-case letters, digits and \"_\", not ending in \"_\"", p.prefix)
	}
	var entries []Entry
	for _, f := range schema.fields {
		name := envName(p.prefix, f.key)
		value, ok := p.lookup(name)
		if !ok {
			continue
		}
		entries = append(entries, Entry{Key: f.key, Value: value, Origin: "env " + name})
	}
	return entries, nil
}
