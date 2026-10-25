// Package logtest captures [slog] records for tests to assert on.
//
// A [Recorder] is a capture handler for tests and is never the installed logger. It does not
// redact: it stores every attribute as the code under test wrote it. Redaction is asserted through
// the writer [go.vipengele.org/cli/core/logging.Setup] writes to, by passing a buffer as
// [go.vipengele.org/cli/core/logging.Options] Writer and reading what reaches it. A test of
// redaction therefore runs both of its layers, the handler wrapper and the redacting writer, and a
// Recorder is no option or constructor that skips either (ADR-0018).
//
// The package [go.vipengele.org/cli/core/logging] documents the field names and levels a record
// is expected to use.
package logtest

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
)

// Record is one captured log record, flattened.
//
// Attrs maps each attribute's key, qualified by the dot-joined path of the groups it sits in, to
// its resolved value: slog.Group("http", slog.Int("status", 200)) is stored as "http.status". When
// two attributes flatten to the same key, a duplicate key or a group key such as "http.status"
// beside a literal key "http.status", the map holds the last value written.
type Record struct {
	// Level is the record's level.
	Level slog.Level
	// Message is the log record's message, [slog.Record.Message]. It is not a Message in the
	// sense of CONTEXT.md, a line a command writes to the standard error.
	Message string
	// Attrs holds the record's attributes and those of the handler that received it, keyed by
	// their dot-joined path.
	Attrs map[string]any
}

// store is the record list every handler derived from one [Recorder] appends to.
type store struct {
	mu      sync.Mutex
	records []Record
}

// attr is an attribute flattened to its qualified key.
type attr struct {
	key   string
	value any
}

// Recorder is a [slog.Handler] that keeps every record it handles. Handlers derived from it with
// WithAttrs and WithGroup share its store, so [Recorder.Records] on any of them returns the
// records of all. A Recorder is safe for concurrent use.
type Recorder struct {
	store  *store
	prefix string
	attrs  []attr
}

// New returns a Recorder with an empty store.
func New() *Recorder {
	return &Recorder{store: &store{}}
}

// Enabled reports true for every level.
func (r *Recorder) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle stores rec, its attributes flattened beneath those r carries.
func (r *Recorder) Handle(_ context.Context, rec slog.Record) error {
	flat := make([]attr, 0, len(r.attrs)+rec.NumAttrs())
	flat = append(flat, r.attrs...)
	rec.Attrs(func(a slog.Attr) bool {
		flat = appendAttr(flat, r.prefix, a)
		return true
	})
	attrs := make(map[string]any, len(flat))
	for _, a := range flat {
		attrs[a.key] = a.value
	}

	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.records = append(r.store.records, Record{Level: rec.Level, Message: rec.Message, Attrs: attrs})
	return nil
}

// WithAttrs returns a handler that adds attrs, qualified by r's groups, to every record. With no
// attrs it returns r.
func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return r
	}
	flat := slices.Clone(r.attrs)
	for _, a := range attrs {
		flat = appendAttr(flat, r.prefix, a)
	}
	return &Recorder{store: r.store, prefix: r.prefix, attrs: flat}
}

// WithGroup returns a handler that qualifies the keys of later attributes by name. An empty name
// returns r.
func (r *Recorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}
	return &Recorder{store: r.store, prefix: r.prefix + name + ".", attrs: r.attrs}
}

// Records returns the records handled so far by r and every handler sharing its store, oldest
// first. The returned slice and its Attrs maps are copies, so a later record or Reset leaves them
// unchanged.
func (r *Recorder) Records() []Record {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	out := make([]Record, len(r.store.records))
	for i, rec := range r.store.records {
		rec.Attrs = maps.Clone(rec.Attrs)
		out[i] = rec
	}
	return out
}

// Reset discards every record in the store r shares.
func (r *Recorder) Reset() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.records = nil
}

// appendAttr appends a, resolved and flattened beneath prefix, to flat. An attribute with an empty
// key contributes nothing unless it is a group, whose members are then inlined without a prefix
// of their own; a group with no members contributes nothing.
func appendAttr(flat []attr, prefix string, a slog.Attr) []attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup {
		if a.Key == "" {
			return flat
		}
		return append(flat, attr{key: prefix + a.Key, value: a.Value.Any()})
	}
	if a.Key != "" {
		prefix += a.Key + "."
	}
	for _, member := range a.Value.Group() {
		flat = appendAttr(flat, prefix, member)
	}
	return flat
}
