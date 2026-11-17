package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Keys are dotted paths of segments, one segment per level of struct nesting: "server.max_retries".
// The canonical form of a segment is snake_case: lower-case ASCII letters and digits with single
// underscores between words. Matching ignores case, "-" and "_" within a segment, so "maxRetries",
// "MAX_RETRIES" and "max-retries" all name "max_retries"; "." always separates levels and is never
// ignored, inserted or removed.

// A path locates one value within a loaded configuration, inside collections included. It starts
// with the key of a Schema field; each list element below it adds its index in brackets, each map
// entry its key quoted as a Go string (strconv.Quote) in brackets, and each field of an element its
// key relative to the element, after a ".": profiles[2].env["FOO"]. A map key is written verbatim
// within its quotes, a "." or "]" in it included, and is never normalised as keys are matched.

// pathStepKind is what one step of a path selects.
type pathStepKind int

const (
	stepKey    pathStepKind = iota // a field, by its dotted key
	stepIndex                      // an element of a list, by its index
	stepMapKey                     // an entry of a map, by its key
)

// pathStep is one step of a path. Consecutive fields are one stepKey, their keys joined by ".", as
// in a Schema key.
type pathStep struct {
	kind  pathStepKind
	key   string // the dotted key of a stepKey, or the map key of a stepMapKey
	index int    // the list index of a stepIndex
}

func keyStep(key string) pathStep    { return pathStep{kind: stepKey, key: key} }
func indexStep(index int) pathStep   { return pathStep{kind: stepIndex, index: index} }
func mapKeyStep(key string) pathStep { return pathStep{kind: stepMapKey, key: key} }

// formatPath returns the display form of a path, such as profiles[2].env["FOO"].
func formatPath(steps []pathStep) string {
	var b strings.Builder
	for i, s := range steps {
		switch s.kind {
		case stepKey:
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.key)
		case stepIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case stepMapKey:
			b.WriteByte('[')
			b.WriteString(strconv.Quote(s.key))
			b.WriteByte(']')
		}
	}
	return b.String()
}

// parsePath returns the steps of path, the form formatPath writes, joining consecutive key segments
// into one stepKey. A path starts with a key segment; each segment is a name validName accepts, an
// index is decimal digits with no leading zero, and a map key is a double-quoted Go string.
func parsePath(path string) ([]pathStep, error) {
	var steps []pathStep
	segment := true // whether a key segment is due: at the start and after a "."
	for i := 0; i < len(path); {
		if segment {
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			if !validName(path[i:j]) {
				return nil, fmt.Errorf("path %q: %q at offset %d is not a key segment", path, path[i:j], i)
			}
			if n := len(steps); n > 0 && steps[n-1].kind == stepKey {
				steps[n-1].key += "." + path[i:j]
			} else {
				steps = append(steps, keyStep(path[i:j]))
			}
			i, segment = j, false
			continue
		}
		switch path[i] {
		case '.':
			i++
			segment = true
		case '[':
			step, n, err := parseBracket(path[i:])
			if err != nil {
				return nil, fmt.Errorf("path %q: at offset %d: %w", path, i, err)
			}
			steps = append(steps, step)
			i += n
		default:
			return nil, fmt.Errorf("path %q: %q at offset %d follows a \"]\"; want \".\" or \"[\"", path, path[i], i)
		}
	}
	if segment {
		return nil, fmt.Errorf("path %q: a key segment is missing at its end", path)
	}
	return steps, nil
}

// parseBracket parses the bracketed step at the start of s, which begins with "[", and returns it
// with the number of bytes it spans.
func parseBracket(s string) (pathStep, int, error) {
	if len(s) > 1 && s[1] == '"' {
		quoted, err := strconv.QuotedPrefix(s[1:])
		if err != nil {
			return pathStep{}, 0, fmt.Errorf("a map key is not a double-quoted string: %w", err)
		}
		end := 1 + len(quoted)
		if end >= len(s) || s[end] != ']' {
			return pathStep{}, 0, fmt.Errorf("the map key %s is not followed by \"]\"", quoted)
		}
		key, _ := strconv.Unquote(quoted) // QuotedPrefix returns only text Unquote accepts
		return mapKeyStep(key), end + 1, nil
	}
	end := 1
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	digits := s[1:end]
	switch {
	case digits == "":
		return pathStep{}, 0, fmt.Errorf("a bracket holds neither an index nor a double-quoted map key")
	case len(digits) > 1 && digits[0] == '0':
		return pathStep{}, 0, fmt.Errorf("the index %s has a leading zero", digits)
	case end >= len(s) || s[end] != ']':
		return pathStep{}, 0, fmt.Errorf("the index %s is not followed by \"]\"", digits)
	}
	index, err := strconv.Atoi(digits)
	if err != nil {
		return pathStep{}, 0, fmt.Errorf("the index %s: %w", digits, err)
	}
	return indexStep(index), end + 1, nil
}

// validName reports whether name, a config tag or a Go field name, can become a key segment: it
// starts with an ASCII letter and holds only ASCII letters, digits, "-" and "_".
func validName(name string) bool {
	if name == "" || !isLetter(name[0]) {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !isLetter(c) && !isDigit(c) && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// validDottedKey reports whether key is one or more "."-separated segments validName accepts.
func validDottedKey(key string) bool {
	for _, segment := range strings.Split(key, ".") {
		if !validName(segment) {
			return false
		}
	}
	return true
}

// snakeCase returns the canonical segment for a name validName accepts. A word boundary is a "-" or
// "_", a lower-case letter or digit followed by an upper-case letter ("maxRetries"), or the last
// upper-case letter of a run followed by a lower-case one ("HTTPServer" is "http_server").
// Consecutive boundaries collapse to one underscore, and none is kept at either end.
func snakeCase(name string) string {
	var b strings.Builder
	boundary := false
	for i := range len(name) {
		c := name[i]
		if c == '-' || c == '_' {
			boundary = true
			continue
		}
		if isUpper(c) && i > 0 {
			prev := name[i-1]
			nextLower := i+1 < len(name) && isLower(name[i+1])
			if isLower(prev) || isDigit(prev) || (isUpper(prev) && nextLower) {
				boundary = true
			}
		}
		if boundary && b.Len() > 0 {
			b.WriteByte('_')
		}
		boundary = false
		b.WriteByte(toLower(c))
	}
	return b.String()
}

// matchKey returns the form two keys share exactly when they match: every segment lower-cased with
// "-" and "_" removed, the "." between segments kept.
func matchKey(key string) string {
	var b strings.Builder
	b.Grow(len(key)) // [lydite:exclude_from_mutation][Grow only reserves capacity; the string built is the same without it]
	for i := range len(key) {
		c := key[i]
		if c == '-' || c == '_' {
			continue
		}
		b.WriteByte(toLower(c))
	}
	return b.String()
}

// envName returns the environment variable of a canonical key: the key upper-cased with every "."
// replaced by "_", after prefix and an underscore when prefix is not empty. With prefix "APP",
// "server.max_retries" is APP_SERVER_MAX_RETRIES.
func envName(prefix, key string) string {
	name := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if prefix == "" {
		return name
	}
	return prefix + "_" + name
}

// flagName returns the command-line flag of a canonical key: every "_" replaced by "-", every "."
// kept, so that each level of nesting stays visible. "server.max_retries" is server.max-retries.
func flagName(key string) string {
	return strings.ReplaceAll(key, "_", "-")
}

// validPrefix reports whether prefix can start an environment variable name: empty, or an
// upper-case ASCII letter followed by upper-case letters, digits and "_", not ending in "_".
func validPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	if !isUpper(prefix[0]) || prefix[len(prefix)-1] == '_' {
		return false
	}
	for i := range len(prefix) {
		c := prefix[i]
		if !isUpper(c) && !isDigit(c) && c != '_' {
			return false
		}
	}
	return true
}

func isUpper(c byte) bool  { return 'A' <= c && c <= 'Z' }
func isLower(c byte) bool  { return 'a' <= c && c <= 'z' }
func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return isUpper(c) || isLower(c) }

func toLower(c byte) byte {
	if isUpper(c) {
		return c + ('a' - 'A')
	}
	return c
}
