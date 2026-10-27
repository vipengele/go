package asset

import (
	"errors"
	"fmt"
	"strings"
)

// DefaultTemplate is the archive template a tool that names none uses.
const DefaultTemplate = "{name}_{os}_{arch}.{ext}"

// DefaultChecksumsTemplate is the checksums template a tool that names none uses.
const DefaultChecksumsTemplate = "checksums.txt"

// DefaultExt is the archive extension a tool that names none uses.
const DefaultExt = "tar.gz"

// extPlaceholder is the placeholder for the archive extension, which a checksums template and a
// GoReleaser name_template cannot carry except as the archive's trailing ".{ext}".
const extPlaceholder = "ext"

// escapedBrace is a GoReleaser action that writes a literal "{". Every "{" of literal text is
// written through it, so no run of braces in a binary name, alone or followed by an action,
// opens an action of its own.
const escapedBrace = `{{ "{" }}`

var (
	// ErrEmptyTemplate reports a template that names nothing.
	ErrEmptyTemplate = errors.New("empty template")
	// ErrUnknownPlaceholder reports a placeholder outside the template language.
	ErrUnknownPlaceholder = errors.New("unknown placeholder")
	// ErrUnclosedPlaceholder reports a "{" with no "}" after it.
	ErrUnclosedPlaceholder = errors.New("unclosed placeholder")
	// ErrMisplacedExt reports an {ext} where the template it appears in cannot carry one.
	ErrMisplacedExt = errors.New("misplaced {ext}")
)

// Fields are the values a template's placeholders stand for.
type Fields struct {
	// Name is the binary name, for {name}.
	Name string
	// Tag is the release tag as published, for {tag}; {version} is Tag without its leading "v".
	Tag string
	// OS is the target operating system, for {os}.
	OS string
	// Arch is the target architecture, for {arch}.
	Arch string
	// Ext is the archive extension, for {ext}.
	Ext string
}

// segment is one piece of a parsed template: literal text, or the name of a placeholder.
type segment struct {
	text        string
	placeholder bool
}

// Validate returns nil when tmpl is a template [Render] accepts: it is not empty, and every "{"
// opens a known placeholder that a "}" closes. The error wraps [ErrEmptyTemplate],
// [ErrUnknownPlaceholder] or [ErrUnclosedPlaceholder].
func Validate(tmpl string) error {
	_, err := parse(tmpl)
	return err
}

// UsesExt reports whether tmpl holds an {ext} placeholder. A template [Validate] rejects holds
// none: UsesExt reports false for it, whatever text it carries.
func UsesExt(tmpl string) bool {
	segs, err := parse(tmpl)
	return err == nil && hasExt(segs)
}

// ValidateChecksums returns nil when tmpl is a valid checksums template: one [Validate] accepts
// that holds no {ext}. The error wraps any error of Validate, or [ErrMisplacedExt].
func ValidateChecksums(tmpl string) error {
	_, err := parseChecksums(tmpl)
	return err
}

// Render returns tmpl with every placeholder replaced by its value from f, in one left-to-right
// pass that never expands a substituted value again. It accepts {ext} anywhere. It fails, as
// [Validate] does, on a template Validate rejects.
func Render(tmpl string, f Fields) (string, error) {
	segs, err := parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, s := range segs {
		if s.placeholder {
			b.WriteString(value(s.text, f))
			continue
		}
		b.WriteString(s.text)
	}
	return b.String(), nil
}

// GoReleaserArchive returns the GoReleaser archive name_template that names the same file as
// tmpl for the binary name. The name is inlined as literal text, a trailing ".{ext}" is dropped
// because GoReleaser appends the extension of the archive's format, and every other placeholder
// becomes the matching GoReleaser field. It fails on a template [Validate] rejects, with
// [ErrMisplacedExt] on an {ext} anywhere but the trailing ".{ext}", and with [ErrEmptyTemplate]
// when nothing but the extension remains.
func GoReleaserArchive(tmpl, name string) (string, error) {
	segs, err := parse(tmpl)
	if err != nil {
		return "", err
	}
	segs = dropTrailingExt(segs)
	if len(segs) == 0 {
		return "", fmt.Errorf("asset: template %q names nothing but its extension: %w", tmpl, ErrEmptyTemplate)
	}
	return goreleaser(tmpl, segs, name)
}

// GoReleaserChecksum returns the GoReleaser checksum name_template that names the same file as
// the checksums template tmpl for the binary name, as [GoReleaserArchive] does for an archive.
// It fails on a template [ValidateChecksums] rejects, so on any {ext}.
func GoReleaserChecksum(tmpl, name string) (string, error) {
	segs, err := parseChecksums(tmpl)
	if err != nil {
		return "", err
	}
	return goreleaser(tmpl, segs, name)
}

// parse splits tmpl into literal text and placeholders, failing on an empty template, an unknown
// placeholder and an unclosed one.
func parse(tmpl string) ([]segment, error) {
	if tmpl == "" {
		return nil, fmt.Errorf("asset: %w", ErrEmptyTemplate)
	}
	var segs []segment
	rest := tmpl
	for rest != "" {
		literal, afterOpen, found := strings.Cut(rest, "{")
		if literal != "" {
			segs = append(segs, segment{text: literal})
		}
		if !found {
			break
		}
		offset := len(tmpl) - len(rest) + len(literal)
		name, after, closed := strings.Cut(afterOpen, "}")
		if !closed {
			return nil, fmt.Errorf("asset: template %q: %w at offset %d", tmpl, ErrUnclosedPlaceholder, offset)
		}
		if !known(name) {
			return nil, fmt.Errorf("asset: template %q: %w {%s} at offset %d", tmpl, ErrUnknownPlaceholder, name, offset)
		}
		segs = append(segs, segment{text: name, placeholder: true})
		rest = after
	}
	return segs, nil
}

// parseChecksums parses tmpl as [parse] does and fails on any {ext}.
func parseChecksums(tmpl string) ([]segment, error) {
	segs, err := parse(tmpl)
	if err != nil {
		return nil, err
	}
	if hasExt(segs) {
		return nil, fmt.Errorf("asset: checksums template %q: %w: a checksums file has no archive extension", tmpl, ErrMisplacedExt)
	}
	return segs, nil
}

// known reports whether name is a placeholder of the template language.
func known(name string) bool {
	switch name {
	case "name", "version", "tag", "os", "arch", extPlaceholder:
		return true
	default:
		return false
	}
}

// value returns the value from f that the known placeholder name stands for.
func value(name string, f Fields) string {
	switch name {
	case "name":
		return f.Name
	case "version":
		return strings.TrimPrefix(f.Tag, "v")
	case "tag":
		return f.Tag
	case "os":
		return f.OS
	case "arch":
		return f.Arch
	default:
		return f.Ext
	}
}

// goreleaserField returns the GoReleaser action that the known placeholder name stands for, and
// the empty string for {name} and {ext}, which have none.
func goreleaserField(name string) string {
	switch name {
	case "version":
		return "{{ .Version }}"
	case "tag":
		return "{{ .Tag }}"
	case "os":
		return "{{ .Os }}"
	case "arch":
		return "{{ .Arch }}"
	default:
		return ""
	}
}

// dropTrailingExt removes a trailing ".{ext}" from segs: the {ext} placeholder and the "." that
// ends the literal text before it. Any other segs are returned unchanged.
func dropTrailingExt(segs []segment) []segment {
	n := len(segs)
	if n < 2 {
		return segs
	}
	last, prev := segs[n-1], segs[n-2]
	if !last.placeholder || last.text != extPlaceholder || prev.placeholder || !strings.HasSuffix(prev.text, ".") {
		return segs
	}
	out := append([]segment(nil), segs[:n-2]...)
	if text := strings.TrimSuffix(prev.text, "."); text != "" {
		out = append(out, segment{text: text})
	}
	return out
}

// hasExt reports whether segs holds an {ext} placeholder.
func hasExt(segs []segment) bool {
	for _, s := range segs {
		if s.placeholder && s.text == extPlaceholder {
			return true
		}
	}
	return false
}

// goreleaser writes segs as a GoReleaser name_template, with name inlined for {name} and every
// "{" of literal text escaped. It fails with [ErrMisplacedExt] on an {ext}.
func goreleaser(tmpl string, segs []segment, name string) (string, error) {
	var b strings.Builder
	for _, s := range segs {
		switch {
		case !s.placeholder:
			writeLiteral(&b, s.text)
		case s.text == "name":
			writeLiteral(&b, name)
		default:
			action := goreleaserField(s.text)
			if action == "" {
				return "", fmt.Errorf("asset: template %q: %w: GoReleaser appends the extension only at the end, after a %q", tmpl, ErrMisplacedExt, ".")
			}
			b.WriteString(action)
		}
	}
	return b.String(), nil
}

// writeLiteral writes text to b with every "{" written as [escapedBrace], so GoReleaser reads it
// as text and never as the start of an action.
func writeLiteral(b *strings.Builder, text string) {
	b.WriteString(strings.ReplaceAll(text, "{", escapedBrace))
}
