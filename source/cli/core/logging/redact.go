package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
)

// Redaction names the secrets a tool adds to the ones every logger redacts: the header names
// Authorization, Proxy-Authorization, Cookie and Set-Cookie, and the values "Bearer <token>" and
// "Basic <token>". A tool adds to that baseline and cannot remove from it, so the zero Redaction
// still redacts it.
//
// A line of text is read by its shape, whatever the log format. A line that is a JSON object or
// array, starting with "{" or "[" after any leading white space, is read as JSON: the value after "Name:" inside
// a string, and the string value of the key "Name", end at the closing unescaped quote, so the
// line stays valid JSON. Any other line is read as plain text: the value after "Name:", and after
// the quoted key form `"Name":` of a JSON fragment, runs to the end of the line.
type Redaction struct {
	// Prefixes are token prefixes, such as "ghp_". The prefix followed by a run of the characters
	// [A-Za-z0-9_+/=.~-] becomes the prefix followed by "[REDACTED]"; a prefix with none of those
	// characters after it is left alone. An empty prefix is ignored.
	Prefixes []string
	// Headers are header names, matched in any letter case. The value of an attribute whose key is
	// one of them, at any group depth, and the value after "Name:" in text, or after the key
	// "Name" in JSON, become "[REDACTED]". An empty name is ignored.
	Headers []string
}

// redacted replaces every secret the redactor finds.
const redacted = "[REDACTED]"

// tokenChars is the character class of the token that follows a prefix.
const tokenChars = `[A-Za-z0-9_+/=.~-]`

// baselinePrefixes and baselineHeaders return what every redactor redacts, whatever its
// [Redaction].
func baselinePrefixes() []string { return []string{"Bearer ", "Basic "} }

func baselineHeaders() []string {
	return []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie"}
}

// lineShape selects how [redactor.lineAs] reads a line.
type lineShape int

const (
	// shapeText reads a line as plain text: the value of "Name: value", and of the quoted key form
	// `"Name": value` a fragment of JSON carries, runs to the end of the line.
	shapeText lineShape = iota
	// shapeJSON reads a line as JSON: the value of "Name: value" inside a string, and the string
	// value of the key "Name", end at the closing unescaped quote, so the line stays valid JSON.
	shapeJSON
)

// shapeOf returns the shape of the whole line s: [shapeJSON] when s, without its surrounding
// white space, starts with "{" or "[" and is valid JSON, and [shapeText] otherwise.
func shapeOf(s []byte) lineShape {
	s = bytes.TrimSpace(s)
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') && json.Valid(s) {
		return shapeJSON
	}
	return shapeText
}

// shapeOfStart returns the shape of a line known only by its start s: [shapeJSON] when s is a
// whole JSON line by [shapeOf], or when s, without its leading white space, starts with "{" and is
// the start of a JSON document, every token in it well formed and only the rest of the document
// missing; [shapeText] otherwise, including when s is all white space. A line that merely starts
// with "{", such as a Go %+v dump or "{Cookie: ...", is text: read as JSON, a header value in it
// would end at its first quote and the rest of the secret would be written. An unfinished "[" is
// text too, as "[" starts many plain log lines, such as "[INFO] ...".
func shapeOfStart(s []byte) lineShape {
	if shapeOf(s) == shapeJSON {
		return shapeJSON
	}
	s = bytes.TrimLeft(s, " \t\r")
	if len(s) == 0 || s[0] != '{' {
		return shapeText
	}
	dec := json.NewDecoder(bytes.NewReader(s))
	for {
		if _, err := dec.Token(); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return shapeJSON
			}
			return shapeText
		}
	}
}

// redactor redacts the secrets of a [Redaction] and the baseline from text and from [slog]
// attributes. It is immutable after [newRedactor] and safe for concurrent use.
type redactor struct {
	// headers holds every header name in lower case.
	headers map[string]struct{}
	// prefix matches a prefix, in group 1, followed by its token.
	prefix *regexp.Regexp
	// textHeader and jsonHeader match a header name and its separator, in group 1, followed by
	// the value the shape delimits.
	textHeader *regexp.Regexp
	jsonHeader *regexp.Regexp
}

// newRedactor returns a redactor for the baseline and the additions of red.
func newRedactor(red Redaction) *redactor {
	prefixes := nonEmpty(append(baselinePrefixes(), red.Prefixes...))
	names := nonEmpty(append(baselineHeaders(), red.Headers...))

	headers := make(map[string]struct{}, len(names))
	for _, name := range names {
		headers[strings.ToLower(name)] = struct{}{}
	}

	// The alternations try the longest literal first, so a prefix or name that extends another
	// matches whole: Proxy-Authorization ahead of Authorization.
	prefixAlt := alternation(prefixes)
	nameAlt := alternation(names)
	return &redactor{
		headers:    headers,
		prefix:     regexp.MustCompile(`(` + prefixAlt + `)` + tokenChars + `+`),
		textHeader: regexp.MustCompile(`(?i)(\b(?:` + nameAlt + `)"?\s*:\s*)[^\r\n]*`),
		jsonHeader: regexp.MustCompile(`(?i)(\b(?:` + nameAlt + `)(?:"\s*:\s*"|\s*:\s*))(?:[^"\\]|\\.)*`),
	}
}

// nonEmpty returns the distinct non-empty strings of ss.
func nonEmpty(ss []string) []string {
	out := slices.DeleteFunc(slices.Clone(ss), func(s string) bool { return s == "" })
	slices.Sort(out)
	return slices.Compact(out)
}

// alternation returns a regular expression alternation of the literals ss, longest first.
func alternation(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = regexp.QuoteMeta(s)
	}
	slices.SortStableFunc(quoted, func(a, b string) int { return len(b) - len(a) })
	return strings.Join(quoted, "|")
}

// isHeader reports whether key is a header name the redactor redacts, in any letter case.
func (r *redactor) isHeader(key string) bool {
	_, ok := r.headers[strings.ToLower(key)]
	return ok
}

// line returns the whole line s with every secret replaced, reading s by its [shapeOf].
func (r *redactor) line(s string) string { return r.lineAs(s, shapeOf([]byte(s))) }

// lineAs returns s with every secret replaced, reading s as shape: header values first, then
// prefixed tokens. Redacting its own output again changes nothing, as "[REDACTED]" is neither a
// token nor a header name.
func (r *redactor) lineAs(s string, shape lineShape) string {
	header := r.textHeader
	if shape == shapeJSON {
		header = r.jsonHeader
	}
	s = header.ReplaceAllString(s, "${1}"+redacted)
	return r.prefix.ReplaceAllString(s, "${1}"+redacted)
}

// text returns s, a message or an attribute's string value, with every secret replaced, read by
// its shape as a line is.
func (r *redactor) text(s string) string { return r.line(s) }

// attr returns a with its value resolved and every secret in it replaced, at any group depth. The
// whole value of an attribute whose key is a header name becomes "[REDACTED]". A value of kind
// [slog.KindAny] is redacted through its formatted form: Error for an error, String for a
// [fmt.Stringer], and %+v otherwise; it is replaced by the redacted string only when that differs
// from the formatted form, so a value with no secret reaches the handler unchanged.
func (r *redactor) attr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if r.isHeader(a.Key) {
		return slog.String(a.Key, redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(r.text(a.Value.String()))
	case slog.KindGroup:
		a.Value = slog.GroupValue(r.attrs(a.Value.Group())...)
	case slog.KindAny:
		formatted := format(a.Value.Any())
		if clean := r.text(formatted); clean != formatted {
			a.Value = slog.StringValue(clean)
		}
	default:
	}
	return a
}

// attrs returns as with every attribute redacted by [redactor.attr].
func (r *redactor) attrs(as []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = r.attr(a)
	}
	return out
}

// format returns the text form of v that a handler writes: Error for an error, String for a
// [fmt.Stringer], %+v otherwise. Formatting goes through fmt, which turns a panicking Error or
// String, such as one on a nil receiver, into text instead of a panic in the handler.
func format(v any) string {
	switch v.(type) {
	case error, fmt.Stringer:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%+v", v)
	}
}

// redactingHandler is a [slog.Handler] that redacts the message and every attribute of a record,
// and every attribute given to WithAttrs, before handing them to next.
type redactingHandler struct {
	next slog.Handler
	r    *redactor
}

// newRedactingHandler returns a handler that redacts with r and writes through next.
func newRedactingHandler(next slog.Handler, r *redactor) slog.Handler {
	return &redactingHandler{next: next, r: r}
}

// Enabled reports whether next handles records at level.
func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle hands next a copy of rec with its message and attributes redacted.
func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.text(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

// WithAttrs returns a handler whose next carries attrs, redacted.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{next: h.next.WithAttrs(h.r.attrs(attrs)), r: h.r}
}

// WithGroup returns a handler whose next opens the group name.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name), r: h.r}
}
