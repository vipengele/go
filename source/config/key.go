package config

import "strings"

// Keys are dotted paths of segments, one segment per level of struct nesting: "server.max_retries".
// The canonical form of a segment is snake_case: lower-case ASCII letters and digits with single
// underscores between words. Matching ignores case, "-" and "_" within a segment, so "maxRetries",
// "MAX_RETRIES" and "max-retries" all name "max_retries"; "." always separates levels and is never
// ignored, inserted or removed.

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
