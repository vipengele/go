package logtest

import (
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// RequireRecord returns the first record r holds at exactly level with exactly message whose Attrs
// contain every pair of attrs, and fails t with a dump of every captured record when there is
// none. attrs may be nil, which matches on level and message alone.
//
// A value compares to the resolved attribute value with [reflect.DeepEqual], so the type must
// match: slog.Int("n", 1) is stored as an int64, and a wanted int 1 does not match it.
func (r *Recorder) RequireRecord(t testing.TB, level slog.Level, message string, attrs map[string]any) Record {
	t.Helper()
	records := r.Records()
	for _, rec := range records {
		if rec.Level == level && rec.Message == message && containsAttrs(rec.Attrs, attrs) {
			return rec
		}
	}
	t.Fatalf("no record at level %v with message %q and attrs %s; captured records:\n%s",
		level, message, formatAttrs(attrs), dump(records))
	return Record{}
}

// RequireNone fails t with a dump of every captured record when r holds a record at or above level.
func (r *Recorder) RequireNone(t testing.TB, level slog.Level) {
	t.Helper()
	records := r.Records()
	for _, rec := range records {
		if rec.Level >= level {
			t.Fatalf("record at level %v with message %q is at or above %v; captured records:\n%s",
				rec.Level, rec.Message, level, dump(records))
			return
		}
	}
}

// containsAttrs reports whether have holds every key of want with a DeepEqual value.
func containsAttrs(have, want map[string]any) bool {
	for key, w := range want {
		h, ok := have[key]
		if !ok || !reflect.DeepEqual(h, w) {
			return false
		}
	}
	return true
}

// formatAttrs renders attrs as key=value (type) pairs in key order.
func formatAttrs(attrs map[string]any) string {
	keys := slices.Sorted(maps.Keys(attrs))
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = fmt.Sprintf("%s=%v (%[2]T)", key, attrs[key])
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// dump renders records one per line, numbered from zero, or "(none)" for no records.
func dump(records []Record) string {
	if len(records) == 0 {
		return "  (none)"
	}
	lines := make([]string, len(records))
	for i, rec := range records {
		lines[i] = fmt.Sprintf("  [%d] %v %q %s", i, rec.Level, rec.Message, formatAttrs(rec.Attrs))
	}
	return strings.Join(lines, "\n")
}
