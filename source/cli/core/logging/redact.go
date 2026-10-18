package logging

import (
	"bytes"
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.vipengele.org/cli/spec"
)

// Redaction names the secrets a tool adds to the ones every logger redacts: the header names
// Authorization, Proxy-Authorization, Cookie and Set-Cookie, and the values "Bearer <token>" and
// "Basic <token>", with the scheme in any letter case, as HTTP reads it. A tool adds to that baseline and cannot remove from it, so the zero Redaction
// still redacts it.
//
// A line of text is read by its shape, whatever the log format. A line that is a JSON object or
// array, starting with "{" or "[" after any leading white space, is read as JSON: the value after
// "Name:" inside a string, and the string value of the key "Name", end at the closing unescaped
// quote, and a value of the key "Name" of any other type, an array, an object, a number, a boolean
// or null, becomes the string "[REDACTED]" whole, so the line stays valid JSON. The key is
// recognised when it is written as the plain string "Name", with no escape in it. Any other line
// is read as plain text: the value after "Name:", and after the quoted key form `"Name":` of a
// JSON fragment, runs to the end of the line. In either reading a name matches whole: after the
// start of the line, a quote or another byte that is neither a word character nor "-", or, inside
// a JSON string, the escape \n, \r or \t, so "X-Authorization: v" and {"X-Cookie":"v"} are left
// alone.
type Redaction struct {
	// Prefixes are token prefixes, such as "ghp_", matched in the letter case they are written in.
	// The prefix followed by a run of the characters [A-Za-z0-9_+/=.~-] becomes the prefix followed
	// by "[REDACTED]"; a prefix with none of those characters after it is left alone. An empty
	// prefix is ignored.
	Prefixes []string
	// Headers are header names, matched in any letter case. The value of an attribute whose key is
	// one of them, at any group depth, and the value after "Name:" in text, or after the key
	// "Name" in JSON, become "[REDACTED]". An empty name is ignored.
	Headers []string
}

// redacted replaces every secret the redactor finds.
const redacted = "[REDACTED]"

// suffixChars is the character class of the run that follows a prefix.
const suffixChars = `[A-Za-z0-9_+/=.~-]`

// baselinePrefixes and baselineHeaders return what every redactor redacts, whatever its
// [Redaction]. A baseline prefix is an HTTP authentication scheme, which matches in any letter
// case.
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
	// value of the key "Name", end at the closing unescaped quote, and a value of the key "Name"
	// of any other type becomes the string "[REDACTED]" whole, so the line stays valid JSON.
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
	// prefix matches a prefix, in group 1, followed by its token: a baseline prefix in any letter
	// case, so group 1 keeps the scheme as it is written, and a prefix of the [Redaction] as given.
	prefix *regexp.Regexp
	// textHeader and jsonHeader match what [nameStart] admits before a header name, in group 1,
	// the name and its separator, in group 2, and the value the shape delimits after them.
	textHeader *regexp.Regexp
	jsonHeader *regexp.Regexp
	// jsonKey matches a header name written as a JSON object key, the quoted name and the colon
	// and white space after it, in group 1. The opening quote follows the start of the text or a
	// byte other than a backslash and then an even run of backslashes, so it is not an escaped
	// quote inside a string. The quote before the name is one of the starts [nameStart] admits, so
	// the key is the whole name, as for textHeader and jsonHeader.
	jsonKey *regexp.Regexp
	// base64String matches a JSON string whose content, in group 1, is a run of the standard
	// base64 alphabet and its padding, the form a [json.Encoder] gives a byte slice. The opening
	// quote follows what jsonKey's does, so it opens a string, and the content holds no backslash,
	// so the closing quote ends that string.
	base64String *regexp.Regexp
}

// nameStart matches what precedes a header name in textHeader and jsonHeader, so a name matches
// only whole: the start of the text, a byte that cannot be part of a header name, neither a word
// character nor "-", or the JSON escape "\n", "\r" or "\t" of a line break or a tab inside a
// string, its letter in lower case as JSON writes it. A longer name that ends in a header name, such as X-Authorization or my_cookie, is not
// that header.
const nameStart = `(?:^|[^\w-]|\\(?-i:[nrt]))`

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
	prefixAlt := alternation(prefixes, prefixPattern)
	nameAlt := alternation(names, regexp.QuoteMeta)
	return &redactor{
		headers:      headers,
		prefix:       regexp.MustCompile(`(` + prefixAlt + `)` + suffixChars + `+`),
		textHeader:   regexp.MustCompile(`(?i)(` + nameStart + `)((?:` + nameAlt + `)"?\s*:\s*)[^\r\n]*`),
		jsonHeader:   regexp.MustCompile(`(?i)(` + nameStart + `)((?:` + nameAlt + `)(?:"\s*:\s*"|\s*:\s*))(?:[^"\\]|\\.)*`),
		jsonKey:      regexp.MustCompile(`(?i)(?:^|[^\\])(?:\\\\)*("(?:` + nameAlt + `)"\s*:\s*)`),
		base64String: regexp.MustCompile(`(?:^|[^\\])(?:\\\\)*"([A-Za-z0-9+/]+=*)"`),
	}
}

// nonEmpty returns the distinct non-empty strings of ss.
func nonEmpty(ss []string) []string {
	out := slices.DeleteFunc(slices.Clone(ss), func(s string) bool { return s == "" })
	slices.Sort(out)
	return slices.Compact(out)
}

// alternation returns a regular expression alternation of the literals ss, longest literal first,
// each written as pattern returns it.
func alternation(ss []string, pattern func(string) string) string {
	sorted := slices.Clone(ss)
	slices.SortStableFunc(sorted, func(a, b string) int { return len(b) - len(a) })
	patterns := make([]string, len(sorted))
	for i, s := range sorted {
		patterns[i] = pattern(s)
	}
	return strings.Join(patterns, "|")
}

// prefixPattern returns the pattern of the prefix s: a baseline prefix in any letter case, and any
// other prefix as written.
func prefixPattern(s string) string {
	if slices.Contains(baselinePrefixes(), s) {
		return `(?i:` + regexp.QuoteMeta(s) + `)`
	}
	return regexp.QuoteMeta(s)
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
		s = r.jsonValues(s)
	}
	s = header.ReplaceAllString(s, "${1}${2}"+redacted)
	return r.prefix.ReplaceAllString(s, "${1}"+redacted)
}

// headerValue is the value of a header key in JSON text that is not a string.
type headerValue struct {
	// start is where the key's match starts, value where the value starts and end where it ends.
	start, value, end int
	// open holds the state of the skip over the value when it runs to the end of the text, and is
	// nil when the value ends inside it.
	open *jsonSkip
}

// headerValues returns, in order, the values of the header keys in the JSON text s that are not
// strings: an array, an object, a number, a boolean or null, including one that runs to the end
// of s. A key whose value has not started before the end of s has none. A string value is left to
// the jsonHeader pattern, which ends it at its closing unescaped quote.
func (r *redactor) headerValues(s string) []headerValue {
	var out []headerValue
	for from := 0; ; {
		m := r.jsonKey.FindStringSubmatchIndex(s[from:])
		if m == nil {
			break
		}
		start, value := from+m[2], from+m[3]
		if value == len(s) || s[value] == '"' {
			from = value
			continue
		}
		end, open := skipJSONValue(s, value)
		out = append(out, headerValue{start: start, value: value, end: end, open: open})
		from = end
	}
	return out
}

// jsonValues returns the JSON text s with every value [redactor.headerValues] finds replaced by
// the string "[REDACTED]".
func (r *redactor) jsonValues(s string) string {
	values := r.headerValues(s)
	if len(values) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, v := range values {
		b.WriteString(s[last:v.value])
		b.WriteString(`"` + redacted + `"`)
		last = v.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// jsonSkip is the state of a skip over one JSON value that is not a string, a byte at a time.
type jsonSkip struct {
	// scalar reports a number, a boolean or null, which ends before the first delimiter; any
	// other value is an array or an object, which ends with the bracket that closes it.
	scalar bool
	// depth is the number of arrays and objects open, inString reports a position inside a
	// string of the value, and escaped one right after a backslash in that string.
	depth             int
	inString, escaped bool
}

// skipStep is where a byte stands relative to the value a [jsonSkip] skips.
type skipStep int

const (
	// skipInside is a byte of the value that is not its last.
	skipInside skipStep = iota
	// skipLast is the last byte of the value, the bracket that closes it.
	skipLast
	// skipAfter is the first byte after the value, the delimiter that ends a scalar or a newline.
	skipAfter
)

// next advances the skip over c and returns where c stands relative to the value. A newline ends
// the value, as it ends the line.
func (k *jsonSkip) next(c byte) skipStep {
	if c == '\n' {
		return skipAfter
	}
	if k.scalar {
		if strings.IndexByte(",}] \t\r", c) >= 0 {
			return skipAfter
		}
		return skipInside
	}
	switch {
	case k.inString && k.escaped:
		k.escaped = false
	case k.inString && c == '\\':
		k.escaped = true
	case k.inString:
		k.inString = c != '"'
	case c == '"':
		k.inString = true
	case c == '{' || c == '[':
		k.depth++
	case c == '}' || c == ']':
		k.depth--
		if k.depth == 0 {
			return skipLast
		}
	}
	return skipInside
}

// skipJSONValue returns where the value that starts at s[i], not a string, ends: after its
// closing bracket, before the delimiter that ends a scalar, or at the end of s, together with
// the state of the skip when the value runs to the end of s.
func skipJSONValue(s string, i int) (int, *jsonSkip) {
	k := &jsonSkip{scalar: s[i] != '{' && s[i] != '['}
	for j := i; j < len(s); j++ {
		switch k.next(s[j]) {
		case skipLast:
			return j + 1, nil
		case skipAfter:
			return j, nil
		case skipInside:
		}
	}
	return len(s), k
}

// text returns s, a message or an attribute's string value, with every secret replaced, read by
// its shape as a line is.
func (r *redactor) text(s string) string { return r.line(s) }

// attr returns a with its value resolved and every secret in it replaced, at any group depth,
// for a handler of format. The whole value of an attribute whose key is a header name becomes
// "[REDACTED]". A value of kind [slog.KindAny] is redacted by [redactor.anyValue].
func (r *redactor) attr(a slog.Attr, format spec.Format) slog.Attr {
	a.Value = a.Value.Resolve()
	if r.isHeader(a.Key) {
		return slog.String(a.Key, redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(r.text(a.Value.String()))
	case slog.KindGroup:
		a.Value = slog.GroupValue(r.attrs(a.Value.Group(), format)...)
	case slog.KindAny:
		a.Value = r.anyValue(a.Value, format)
	default:
	}
	return a
}

// attrs returns as with every attribute redacted by [redactor.attr].
func (r *redactor) attrs(as []slog.Attr, format spec.Format) []slog.Attr {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = r.attr(a, format)
	}
	return out
}

// anyValue returns v, of kind [slog.KindAny], rendered once in the form the [slog] handler of
// format writes and redacted in that form, so the text checked is the text written.
//
// For [spec.FormatJSON] the form is the error text of an error that is not a [json.Marshaler],
// and the value as a [json.Encoder] encodes it otherwise, a [encoding.TextMarshaler] as its text
// in a JSON string; the encoded value is read as JSON, so a header key at any depth has its value
// redacted whatever its type, and it is handed on as a [json.RawMessage], so an object stays an
// object. A string in the encoded value that is the base64 form of bytes holding a secret is
// replaced by those bytes as a JSON string, redacted, by [redactor.decodeBytes]: a [json.Encoder]
// writes a byte slice in base64, at any depth, unless its type is a [json.Marshaler] or a
// [encoding.TextMarshaler], and whatever the value's own marshaling writes is read the same way.
// A byte array the encoder writes as a JSON array of its decimal byte values, at any depth, is
// checked as its bytes, and when they hold a secret that array is replaced by those bytes as a
// JSON string, redacted, by [redactor.jsonBytes]; a value with no such secret is written exactly
// as the encoder writes it.
// For [spec.FormatText] the form is the text of a [encoding.TextAppender] or
// [encoding.TextMarshaler], and the %+v form otherwise, which is the Error or String of an error
// or a [fmt.Stringer]; it is handed on as a string, which the text handler writes as it would
// have written v. A byte slice, which the text handler writes quoted, is checked as its bytes and
// replaced only when they hold a secret. A byte slice or byte array inside the value, which %+v
// writes as its decimal byte values, is checked as its bytes too, and when they hold a secret its
// decimal form is replaced by those bytes, redacted, as a quoted string, by
// [redactor.secretBytes]; a value with no such secret is written exactly as %+v writes it.
//
// A value whose rendering panics is handed on unchanged, and the handler writes its own panic
// text. A value whose rendering fails has the "!ERROR:" text the handler writes for the failure
// checked, and is replaced by it, redacted, only when that text holds a secret.
func (r *redactor) anyValue(v slog.Value, format spec.Format) slog.Value {
	if format == spec.FormatJSON {
		return r.jsonAny(v)
	}
	return r.textAny(v)
}

// jsonAny is [redactor.anyValue] for [spec.FormatJSON].
func (r *redactor) jsonAny(v slog.Value) slog.Value {
	a := v.Any()
	if err, ok := a.(error); ok {
		if _, jm := a.(json.Marshaler); !jm {
			s, err := render(func() (string, error) { return err.Error(), nil })
			if err != nil {
				return v
			}
			return slog.StringValue(r.text(s))
		}
	}
	s, err := render(func() (string, error) { return encodeJSON(a) })
	if err != nil {
		return r.failed(v, err)
	}
	for _, sub := range r.jsonBytes(a) {
		s = replaceOutsideStrings(s, sub.form, sub.clean)
	}
	clean := r.lineAs(r.decodeBytes(s), shapeJSON)
	if clean != s && !json.Valid([]byte(clean)) {
		return slog.StringValue(clean)
	}
	return slog.AnyValue(json.RawMessage(clean))
}

// decodeBytes returns the JSON text s with every string that is the base64 form of bytes holding a
// secret replaced by those bytes as a JSON string, redacted by [redactor.text], and every other
// string as it is. The bytes decoded before an error in the base64 are checked as well, so a
// string with its padding cut still has its secret found.
func (r *redactor) decodeBytes(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range r.base64String.FindAllStringSubmatchIndex(s, -1) {
		raw, _ := base64.StdEncoding.DecodeString(s[m[2]:m[3]])
		clean := r.text(string(raw))
		if clean == string(raw) {
			continue
		}
		quoted, _ := encodeJSON(clean)
		b.WriteString(s[last : m[2]-1])
		b.WriteString(quoted)
		last = m[3] + 1
	}
	b.WriteString(s[last:])
	return b.String()
}

// textAny is [redactor.anyValue] for [spec.FormatText].
func (r *redactor) textAny(v slog.Value) slog.Value {
	a := v.Any()
	var text func() (string, error)
	switch t := a.(type) {
	case encoding.TextAppender:
		text = func() (string, error) {
			b, err := t.AppendText(nil)
			return string(b), err
		}
	case encoding.TextMarshaler:
		text = func() (string, error) {
			b, err := t.MarshalText()
			return string(b), err
		}
	default:
		if bs, ok := byteSlice(a); ok {
			if clean := r.text(string(bs)); clean != string(bs) {
				return slog.StringValue(clean)
			}
			return v
		}
		s := fmt.Sprintf("%+v", a)
		for _, sub := range r.secretBytes(a) {
			s = strings.ReplaceAll(s, sub.form, sub.clean)
		}
		return slog.StringValue(r.text(s))
	}
	s, err := render(text)
	if err != nil {
		return r.failed(v, err)
	}
	return slog.StringValue(r.text(s))
}

// bytesSub replaces, in the %+v or JSON form of a value, the decimal form of a byte slice or byte
// array whose bytes hold a secret, such as "[67 111 111]" or "[67,111,111]", by clean, those bytes
// redacted and quoted.
type bytesSub struct{ form, clean string }

// secretBytes returns a [bytesSub] for each byte slice and byte array inside a, the value %+v
// prints, whose bytes hold a secret. %+v writes such bytes as their decimal values, which no
// pattern matches. The walk reads what %+v reads: exported and unexported fields, the elements of
// slices and arrays, the keys and values of maps, and what an interface holds, and it follows a
// pointer only at the top, as %+v prints "&{...}" there and an address below. It therefore ends
// whenever %+v does: a value that holds itself through a pointer is cut at that pointer by both,
// and one that holds itself through a map, a slice or an interface %+v cannot print either.
func (r *redactor) secretBytes(a any) []bytesSub {
	v := reflect.ValueOf(a)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	var subs []bytesSub
	r.walkBytes(v, &subs)
	return subs
}

// walkBytes appends to subs a [bytesSub] for each byte slice and byte array in v whose bytes hold
// a secret, as [redactor.secretBytes] walks it. Values are read without Interface, so unexported
// fields are read too.
func (r *redactor) walkBytes(v reflect.Value, subs *[]bytesSub) {
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if b, clean, ok := r.heldSecret(v); ok {
				*subs = append(*subs, bytesSub{form: fmt.Sprintf("%d", b), clean: strconv.Quote(clean)})
			}
			return
		}
		for i := range v.Len() {
			r.walkBytes(v.Index(i), subs)
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			r.walkBytes(it.Key(), subs)
			r.walkBytes(it.Value(), subs)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			r.walkBytes(v.Field(i), subs)
		}
	case reflect.Interface:
		r.walkBytes(v.Elem(), subs)
	default:
	}
}

// heldSecret returns the bytes of v, a byte slice or byte array, and those bytes redacted by
// [redactor.text], and reports whether they hold a secret. Each element is copied by SetUint, which
// stores the value of a uint8 element whole. Bytes and Copy do not read every such slice or array:
// Bytes refuses an array that is not addressable, and Copy one read through an unexported field.
func (r *redactor) heldSecret(v reflect.Value) (b []byte, clean string, ok bool) {
	b = make([]byte, v.Len())
	dst := reflect.ValueOf(b)
	for i := range b {
		dst.Index(i).SetUint(v.Index(i).Uint())
	}
	clean = r.text(string(b))
	return b, clean, clean != string(b)
}

// jsonBytes returns a [bytesSub] for each byte array inside a whose bytes hold a secret and which
// a [json.Encoder] writes as a JSON array of its decimal byte values, such as "[67,111,111]",
// with clean those bytes, redacted, as a JSON string. No pattern matches the decimal form. The
// walk reads what the encoder writes, by [jsonWalk.walk].
func (r *redactor) jsonBytes(a any) []bytesSub {
	w := jsonWalk{r: r}
	w.walk(reflect.ValueOf(a))
	return w.subs
}

// jsonWalk is the state of [redactor.jsonBytes]'s walk.
type jsonWalk struct {
	r    *redactor
	subs []bytesSub
	// visited holds every pointer, map and slice the walk has read, so it reads each once and
	// ends on a value that holds itself.
	visited map[jsonVisit]bool
}

// jsonVisit identifies a pointer, map or slice by its address, its type and, for a slice, its
// length: a pointer to a struct and one to its first field share an address, as do two slices of
// one array, and each holds what the other does not.
type jsonVisit struct {
	ptr uintptr
	n   int
	t   reflect.Type
}

// walk appends to w.subs a [bytesSub] for each byte array in v that a [json.Encoder] writes as its
// decimal byte values, following the encoder's rules. A value whose type is a [json.Marshaler]
// or a [encoding.TextMarshaler], or whose pointer type is one when v is addressable, is written by
// its method and not read. A byte slice is written in base64, which [redactor.decodeBytes] reads,
// or element by element by the methods of its element type. A byte array is written as its
// decimal byte values unless its element type has those methods, the pointer's when the array is
// addressable. A struct is read through the fields the encoder writes, by [jsonField]; a map
// through its values, as a key the encoder accepts is a string, a number or the text of a
// [encoding.TextMarshaler]; a pointer and an interface through what they hold.
func (w *jsonWalk) walk(v reflect.Value) {
	if !v.IsValid() || marshals(v.Type(), v.CanAddr()) || w.seen(v) {
		return
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		t := v.Type()
		if t.Elem().Kind() != reflect.Uint8 {
			for i := range v.Len() {
				w.walk(v.Index(i))
			}
		} else if t.Kind() == reflect.Array && !marshals(t.Elem(), v.CanAddr()) {
			if b, clean, ok := w.r.heldSecret(v); ok {
				quoted, _ := encodeJSON(clean)
				w.subs = append(w.subs, bytesSub{form: strings.ReplaceAll(fmt.Sprintf("%d", b), " ", ","), clean: quoted})
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			w.walk(it.Value())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if jsonField(v.Type().Field(i)) {
				w.walk(v.Field(i))
			}
		}
	case reflect.Pointer, reflect.Interface:
		w.walk(v.Elem())
	default:
	}
}

// seen reports whether v, a pointer, a map or a slice, was read before, and records it as read.
// It reports false for a value of any other kind.
func (w *jsonWalk) seen(v reflect.Value) bool {
	key := jsonVisit{t: v.Type()}
	switch v.Kind() {
	case reflect.Slice:
		key.n = v.Len()
		key.ptr = v.Pointer()
	case reflect.Pointer, reflect.Map:
		key.ptr = v.Pointer()
	default:
		return false
	}
	if w.visited[key] {
		return true
	}
	if w.visited == nil {
		w.visited = map[jsonVisit]bool{}
	}
	w.visited[key] = true
	return false
}

// marshals reports whether a [json.Encoder] writes a value of type t by its own MarshalJSON or
// MarshalText: when t has the method, or when the value is addressable and t's pointer type has
// it.
func marshals(t reflect.Type, addressable bool) bool {
	jm := reflect.TypeFor[json.Marshaler]()
	tm := reflect.TypeFor[encoding.TextMarshaler]()
	if t.Implements(jm) || t.Implements(tm) {
		return true
	}
	p := reflect.PointerTo(t)
	return addressable && (p.Implements(jm) || p.Implements(tm))
}

// jsonField reports whether a [json.Encoder] reads the struct field f: an exported field, and an
// embedded struct or pointer to a struct whatever its name, whose exported fields the encoder
// writes, unless the field's tag is `json:"-"`. Where the encoder drops a field whose name
// another field at a shallower depth shares, the walk reads it still, and only finds bytes the
// encoded text does not hold.
func jsonField(f reflect.StructField) bool {
	if f.Tag.Get("json") == "-" {
		return false
	}
	t := f.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return f.IsExported() || f.Anonymous && t.Kind() == reflect.Struct
}

// replaceOutsideStrings returns the JSON text s with every occurrence of old that starts outside
// a string replaced by repl; an occurrence inside a string is text the value holds, not a value of
// its own.
func replaceOutsideStrings(s, old, repl string) string {
	var b strings.Builder
	last := 0
	inString, escaped := false, false
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && strings.HasPrefix(s[i:], old):
			b.WriteString(s[last:i])
			b.WriteString(repl)
			i += len(old)
			last = i
			continue
		}
		i++
	}
	b.WriteString(s[last:])
	return b.String()
}

// failed returns v, whose rendering failed with err, replaced by the "!ERROR:" text a handler
// writes for the failure, redacted, when that text holds a secret, and v unchanged otherwise. A
// rendering that panicked leaves v unchanged.
func (r *redactor) failed(v slog.Value, err error) slog.Value {
	if errors.Is(err, errRenderPanic) {
		return v
	}
	s := "!ERROR:" + err.Error()
	if clean := r.text(s); clean != s {
		return slog.StringValue(clean)
	}
	return v
}

// errRenderPanic is the error of a rendering that panicked.
var errRenderPanic = errors.New("rendering panicked")

// render returns what f returns, and [errRenderPanic] when f panics: the handler then renders the
// value itself and writes its own text for the panic.
func render(f func() (string, error)) (s string, err error) {
	defer func() {
		if recover() != nil {
			s, err = "", errRenderPanic
		}
	}()
	return f()
}

// encodeJSON returns v encoded as the [slog] JSON handler encodes it, by a [json.Encoder] that
// does not escape HTML, without the trailing newline.
func encodeJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// byteSlice returns a as a byte slice when its underlying type is one, as the [slog] text
// handler tests it.
func byteSlice(a any) ([]byte, bool) {
	if bs, ok := a.([]byte); ok {
		return bs, true
	}
	t := reflect.TypeOf(a)
	if t != nil && t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return reflect.ValueOf(a).Bytes(), true
	}
	return nil, false
}

// redactingHandler is a [slog.Handler] that redacts the message and every attribute of a record,
// and every attribute given to WithAttrs, before handing them to next.
type redactingHandler struct {
	next slog.Handler
	r    *redactor
	// format is the format next writes, which decides the form a value of kind [slog.KindAny]
	// is rendered and redacted in.
	format spec.Format
}

// newRedactingHandler returns a handler that redacts with r and writes through next, a [slog]
// handler of format: [slog.JSONHandler] for [spec.FormatJSON] and [slog.TextHandler] otherwise.
func newRedactingHandler(next slog.Handler, r *redactor, format spec.Format) slog.Handler {
	return &redactingHandler{next: next, r: r, format: format}
}

// Enabled reports whether next handles records at level.
func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle hands next a copy of rec with its message and attributes redacted.
func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.text(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.attr(a, h.format))
		return true
	})
	return h.next.Handle(ctx, out)
}

// WithAttrs returns a handler whose next carries attrs, redacted.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{next: h.next.WithAttrs(h.r.attrs(attrs, h.format)), r: h.r, format: h.format}
}

// WithGroup returns a handler whose next opens the group name.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name), r: h.r, format: h.format}
}
