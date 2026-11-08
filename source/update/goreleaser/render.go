package goreleaser

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
)

// The paths of the variables of go.vipengele.org/cli/core/buildinfo that the linker sets, the
// default of each [LinkVars] field.
const (
	DefaultVersionVar = "go.vipengele.org/cli/core/buildinfo.version"
	DefaultCommitVar  = "go.vipengele.org/cli/core/buildinfo.commit"
	DefaultDateVar    = "go.vipengele.org/cli/core/buildinfo.date"
)

// DefaultSigningKeyPath is the minisign secret key file the signs block reads when
// Options.SigningKeyPath is empty, relative to the directory GoReleaser runs in.
const DefaultSigningKeyPath = "minisign.key"

// Platform is one target of the build, as Go names it.
type Platform struct {
	// GOOS is the target operating system, such as "linux".
	GOOS string
	// GOARCH is the target architecture, such as "amd64".
	GOARCH string
}

// LinkVars are the import-path-qualified names of the string variables the build sets with -X:
// the release tag, the full commit and the build date. An empty field means its default.
type LinkVars struct {
	// Version receives the release tag, "v" included. Empty means [DefaultVersionVar].
	Version string
	// Commit receives the full commit hash. Empty means [DefaultCommitVar].
	Commit string
	// Date receives the build date. Empty means [DefaultDateVar].
	Date string
}

// DefaultLinkVars returns the variables of go.vipengele.org/cli/core/buildinfo.
func DefaultLinkVars() LinkVars {
	return LinkVars{Version: DefaultVersionVar, Commit: DefaultCommitVar, Date: DefaultDateVar}
}

// Options are the inputs of the config that an [update.Config] does not carry.
type Options struct {
	// Main is the main package GoReleaser builds, such as "./cmd/tool". It is required.
	Main string
	// Dir is the directory GoReleaser builds in. Empty leaves the dir key out, so GoReleaser builds
	// in its own working directory.
	Dir string
	// Platforms are the targets of the build. Empty means linux and darwin, each on amd64 and
	// arm64. No entry may be empty in either field or listed twice.
	Platforms []Platform
	// LinkVars names the variables the version, commit and date are linked into. A zero field means
	// its [DefaultLinkVars] value.
	LinkVars LinkVars
	// SigningKeyPath is the minisign secret key file the signs block passes to minisign -s. Empty
	// means [DefaultSigningKeyPath]. It is unused under AllowUnsigned, which emits no signs block.
	SigningKeyPath string
}

// configSource is the GoReleaser config with a token, an upper-case name between "@@" pairs, in
// place of each value Render fills. Every token stands where a complete YAML value goes, and Render
// replaces it with a double-quoted scalar or a flow sequence of them, so the template never quotes
// a token. The lines from a *_BEGIN marker line to its *_END line are an optional region.
//
//go:embed goreleaser.yaml.in
var configSource string

// The markers of the optional regions of configSource: the dir key, kept when Options.Dir is set,
// and the signs block, kept unless the Config allows unsigned releases.
const (
	dirBegin    = "@@DIR_BEGIN@@\n"
	dirEnd      = "@@DIR_END@@\n"
	signedBegin = "@@SIGNED_BEGIN@@\n"
	signedEnd   = "@@SIGNED_END@@\n"
)

// windows is the GOOS for which GoReleaser names a binary with ".exe".
const windows = "windows"

// Render returns the GoReleaser v2 config that publishes the releases cfg describes. It resolves
// cfg with [update.Config.Resolved] and fails with its error on an invalid Config, and with the
// error of [asset.GoReleaserArchive] or [asset.GoReleaserChecksum] on a template either refuses.
// It fails with an error wrapping [ErrNoMain] when opts.Main is empty, [ErrEmptyPlatform] or
// [ErrDuplicatePlatform] on a bad entry of opts.Platforms, [ErrPlatformChecksums] when the
// ChecksumsAsset holds {os} or {arch}, [ErrRawWindowsAsset] when the AssetTemplate holds no {ext}
// and a platform is windows, [ErrInvalidLinkVar] on a link variable that cannot be written into
// -X, and [ErrNotUTF8] on any value that is not valid UTF-8.
//
// Every value reaches the config as a double-quoted YAML scalar. A value GoReleaser expands as a
// template, the binary name, the link variables and the signing key path, has each "{" and "$"
// written as a template action that yields it, so it is never read as an action or a variable.
func Render(cfg update.Config, opts Options) ([]byte, error) {
	r, err := cfg.Resolved()
	if err != nil {
		return nil, fmt.Errorf("goreleaser: %w", err)
	}
	if opts.Main == "" {
		return nil, fmt.Errorf("goreleaser: Main: %w", ErrNoMain)
	}
	platforms := opts.Platforms
	if len(platforms) == 0 {
		platforms = defaultPlatforms()
	}
	if err := checkPlatforms(platforms); err != nil {
		return nil, err
	}
	vars := linkVarsOrDefault(opts.LinkVars)
	keyPath := opts.SigningKeyPath
	if keyPath == "" {
		keyPath = DefaultSigningKeyPath
	}
	if err := checkUTF8(r, opts, platforms, vars, keyPath); err != nil {
		return nil, err
	}
	if err := checkLinkVars(vars); err != nil {
		return nil, err
	}
	if holdsPlatform(r.ChecksumsAsset) {
		return nil, fmt.Errorf("goreleaser: ChecksumsAsset %q: %w", r.ChecksumsAsset, ErrPlatformChecksums)
	}
	raw := !asset.UsesExt(r.AssetTemplate)
	if raw {
		for _, p := range platforms {
			if p.GOOS == windows {
				return nil, fmt.Errorf("goreleaser: AssetTemplate %q holds no {ext}: %w", r.AssetTemplate, ErrRawWindowsAsset)
			}
		}
	}
	archiveName, err := asset.GoReleaserArchive(r.AssetTemplate, r.BinaryName)
	if err != nil {
		return nil, fmt.Errorf("goreleaser: AssetTemplate: %w", err)
	}
	checksumName, err := asset.GoReleaserChecksum(r.ChecksumsAsset, r.BinaryName)
	if err != nil {
		return nil, fmt.Errorf("goreleaser: ChecksumsAsset: %w", err)
	}

	goos, goarch, ignore := matrix(platforms)
	ldflags := []string{
		"-s -w",
		"-X " + templateLiteral(vars.Version) + "={{ .Tag }}",
		"-X " + templateLiteral(vars.Commit) + "={{ .FullCommit }}",
		"-X " + templateLiteral(vars.Date) + "={{ .Date }}",
	}
	pairs := []string{
		"@@PROJECT_NAME@@", yamlQuote(r.Name),
		"@@BINARY@@", yamlQuote(templateLiteral(r.BinaryName)),
		"@@MAIN@@", yamlQuote(opts.Main),
		"@@DIR@@", yamlQuote(opts.Dir),
		"@@LDFLAGS@@", yamlList(ldflags),
		"@@GOOS@@", yamlList(goos),
		"@@GOARCH@@", yamlList(goarch),
		"@@IGNORE@@", yamlIgnore(ignore),
		"@@ARCHIVE_NAME@@", yamlQuote(archiveName),
		"@@FORMATS@@", yamlList([]string{archiveFormat(raw, r.AssetExt)}),
		"@@CHECKSUM_NAME@@", yamlQuote(checksumName),
		"@@SIGNING_KEY@@", yamlQuote(templateLiteral(keyPath)),
	}
	src := regions(configSource, dirBegin, dirEnd, opts.Dir != "")
	src = regions(src, signedBegin, signedEnd, !r.AllowUnsigned)
	// One pass of a Replacer never scans the text it inserts, so a token inside a value, such as a
	// main package path holding "@@MAIN@@", stays literal.
	return []byte(strings.NewReplacer(pairs...).Replace(src)), nil
}

// defaultPlatforms returns the targets of a build whose Options name none.
func defaultPlatforms() []Platform {
	return []Platform{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
	}
}

// checkPlatforms rejects a platform with an empty field and one listed earlier.
func checkPlatforms(platforms []Platform) error {
	seen := make(map[Platform]int, len(platforms))
	for i, p := range platforms {
		if p.GOOS == "" || p.GOARCH == "" {
			return fmt.Errorf("goreleaser: Platforms[%d] %q/%q: %w", i, p.GOOS, p.GOARCH, ErrEmptyPlatform)
		}
		if first, dup := seen[p]; dup {
			return fmt.Errorf("goreleaser: Platforms[%d] %s/%s repeats Platforms[%d]: %w", i, p.GOOS, p.GOARCH, first, ErrDuplicatePlatform)
		}
		seen[p] = i
	}
	return nil
}

// linkVarsOrDefault returns v with each empty field set to its [DefaultLinkVars] value.
func linkVarsOrDefault(v LinkVars) LinkVars {
	d := DefaultLinkVars()
	if v.Version == "" {
		v.Version = d.Version
	}
	if v.Commit == "" {
		v.Commit = d.Commit
	}
	if v.Date == "" {
		v.Date = d.Date
	}
	return v
}

// checkLinkVars rejects a variable path that the Go linker's flag parsing would split or end: one
// holding whitespace or a quote, which -ldflags splits on, or "=", which ends the name in -X.
func checkLinkVars(v LinkVars) error {
	for _, f := range []struct{ field, path string }{
		{"LinkVars.Version", v.Version},
		{"LinkVars.Commit", v.Commit},
		{"LinkVars.Date", v.Date},
	} {
		if strings.ContainsFunc(f.path, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'=`, r) }) {
			return fmt.Errorf("goreleaser: %s %q: %w: must hold no whitespace, quote or '='", f.field, f.path, ErrInvalidLinkVar)
		}
	}
	return nil
}

// checkUTF8 rejects any value Render writes into the config that is not valid UTF-8. A YAML
// document is text: such a value has no faithful representation in it.
func checkUTF8(r update.Config, opts Options, platforms []Platform, vars LinkVars, keyPath string) error {
	values := []struct{ field, value string }{
		{"Name", r.Name},
		{"BinaryName", r.BinaryName},
		{"AssetTemplate", r.AssetTemplate},
		{"ChecksumsAsset", r.ChecksumsAsset},
		{"Main", opts.Main},
		{"Dir", opts.Dir},
		{"LinkVars.Version", vars.Version},
		{"LinkVars.Commit", vars.Commit},
		{"LinkVars.Date", vars.Date},
		{"SigningKeyPath", keyPath},
	}
	for i, p := range platforms {
		values = append(values,
			struct{ field, value string }{"Platforms[" + strconv.Itoa(i) + "].GOOS", p.GOOS},
			struct{ field, value string }{"Platforms[" + strconv.Itoa(i) + "].GOARCH", p.GOARCH},
		)
	}
	for _, v := range values {
		if !utf8.ValidString(v.value) {
			return fmt.Errorf("goreleaser: %s %q: %w", v.field, v.value, ErrNotUTF8)
		}
	}
	return nil
}

// holdsPlatform reports whether the valid template tmpl holds {os} or {arch}: whether two renders
// that differ only in the platform differ.
func holdsPlatform(tmpl string) bool {
	a, errA := asset.Render(tmpl, asset.Fields{OS: "a", Arch: "a"})
	b, errB := asset.Render(tmpl, asset.Fields{OS: "b", Arch: "b"})
	return errA == nil && errB == nil && a != b
}

// matrix returns the goos and goarch lists of platforms, each in first-seen order, and every
// goos×goarch pair of those lists that platforms does not hold, ordered by goos then goarch as the
// lists order them. GoReleaser builds every pair of the lists but the ignored ones, which is
// exactly platforms.
func matrix(platforms []Platform) (goos, goarch []string, ignore []Platform) {
	want := make(map[Platform]bool, len(platforms))
	seenOS := make(map[string]bool)
	seenArch := make(map[string]bool)
	for _, p := range platforms {
		want[p] = true
		if !seenOS[p.GOOS] {
			seenOS[p.GOOS] = true
			goos = append(goos, p.GOOS)
		}
		if !seenArch[p.GOARCH] {
			seenArch[p.GOARCH] = true
			goarch = append(goarch, p.GOARCH)
		}
	}
	for _, o := range goos {
		for _, a := range goarch {
			if p := (Platform{GOOS: o, GOARCH: a}); !want[p] {
				ignore = append(ignore, p)
			}
		}
	}
	return goos, goarch, ignore
}

// archiveFormat returns the GoReleaser archive format of a raw asset, "binary", or of an archive
// whose extension is ext. GoReleaser names an archive with "." and its format, so each format is
// the extension update looks for. ext is the AssetExt of a resolved Config, which with a template
// holding {ext} is one of the three cases; any other is returned as is, for GoReleaser to refuse.
func archiveFormat(raw bool, ext string) string {
	if raw {
		return "binary"
	}
	switch ext {
	case "tar.gz":
		return "tar.gz"
	case "tgz":
		return "tgz"
	case "zip":
		return "zip"
	default:
		return ext
	}
}

// regions returns src with each region from a begin line to the next end line reduced to the lines
// between the two markers when keep is true, and removed whole when it is false. A marker is the
// whole of its line, so removing a region leaves src exactly as it would be had the region never
// been written.
func regions(src, begin, end string, keep bool) string {
	var out strings.Builder
	for {
		before, rest, found := strings.Cut(src, begin)
		out.WriteString(before)
		if !found {
			return out.String()
		}
		region, after, _ := strings.Cut(rest, end)
		if keep {
			out.WriteString(region)
		}
		src = after
	}
}

// templateLiteral returns s written so that GoReleaser's template expansion yields s itself: each
// "{" and "$" becomes an action that writes it. A "{" left as is could open an action, and a "$"
// is expanded as an environment variable in the fields of a signs block before the template runs.
func templateLiteral(s string) string {
	return strings.NewReplacer("{", `{{ "{" }}`, "$", `{{ "$" }}`).Replace(s)
}

// yamlList returns values as a YAML flow sequence of double-quoted scalars.
func yamlList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = yamlQuote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// yamlIgnore returns platforms as a YAML flow sequence of goos and goarch mappings, "[]" when there
// are none.
func yamlIgnore(platforms []Platform) string {
	entries := make([]string, len(platforms))
	for i, p := range platforms {
		entries[i] = "{goos: " + yamlQuote(p.GOOS) + ", goarch: " + yamlQuote(p.GOARCH) + "}"
	}
	return "[" + strings.Join(entries, ", ") + "]"
}

// yamlQuote returns s, valid UTF-8, as a double-quoted YAML scalar. A double quote and a backslash
// are backslash-escaped, and every rune a YAML stream may not carry as is, or a parser could fold
// or strip, is written as a \u escape: C0 and C1 controls, tab included, DEL, the line and
// paragraph separators, the byte order mark and the two noncharacters U+FFFE and U+FFFF. Every
// other rune is written as is.
func yamlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20, r >= 0x7f && r <= 0x9f, r == 0x2028, r == 0x2029, r == 0xfeff, r == 0xfffe, r == 0xffff:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
