package spec

import (
	"maps"
	"slices"
	"strings"
)

// CommandInfo is a read-only view of the command an execution runs: its path from the root and
// its annotations. A hook receives it beside the [Invocation]. Every accessor returns a copy, so
// no caller can change what another one sees. The zero value is a command with an empty path and
// no annotations.
type CommandInfo struct {
	path        []string
	annotations map[string]string
}

// NewCommandInfo returns the view of the command at path, the names from the root to the
// command, carrying annotations. path and annotations are copied, so the caller may reuse them.
func NewCommandInfo(path []string, annotations map[string]string) CommandInfo {
	return CommandInfo{
		path:        slices.Clone(path),
		annotations: maps.Clone(annotations),
	}
}

// Path returns the names from the root to the command: "tool", "remote" and "add" for the
// command "tool remote add".
func (c CommandInfo) Path() []string { return slices.Clone(c.path) }

// String returns the path joined by spaces, as in "tool remote add".
func (c CommandInfo) String() string { return strings.Join(c.path, " ") }

// Annotation returns the value of the annotation key and whether the command carries it.
func (c CommandInfo) Annotation(key string) (string, bool) {
	v, ok := c.annotations[key]
	return v, ok
}

// Annotations returns every annotation of the command. It is never nil.
func (c CommandInfo) Annotations() map[string]string {
	m := maps.Clone(c.annotations)
	if m == nil {
		m = map[string]string{}
	}
	return m
}
