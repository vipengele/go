package installscript

import (
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/internal/minisign"
)

// Suffixes of the environment variables the scripts read, each joined to the EnvPrefix by
// [spec.Identity.EnvVar].
const (
	versionEnvSuffix    = "VERSION"
	installDirEnvSuffix = "INSTALL_DIR"
	baseURLEnvSuffix    = "BASE_URL"
	// allowUnsignedEnvSuffix names the variable that, set to 1, lets a script rendered with
	// trusted keys install on the checksum alone when minisign is not installed.
	allowUnsignedEnvSuffix = "ALLOW_UNSIGNED"
	// noModifyPathEnvSuffix names the variable that, set to 1, keeps the PowerShell script from
	// adding the install directory to PATH.
	noModifyPathEnvSuffix = "NO_MODIFY_PATH"
)

// The GOOS each renderer resolves a Config with when the Config names none. The sh script detects
// the system when it runs, so the GOOS it resolves with decides only the defaults, AssetExt among
// them; any GOOS but "windows" gives the same script, and "linux" stands for them all.
const (
	shGOOS         = "linux"
	powerShellGOOS = "windows"
)

// The lines from signedBegin to signedEnd, both marker lines included, are the signature check:
// Render keeps what lies between them for a Config with TrustedKeys and drops it all under
// AllowUnsigned, so a script for unsigned releases holds no trace of the check.
const (
	signedBegin = "@@SIGNED_BEGIN@@\n"
	signedEnd   = "@@SIGNED_END@@\n"
)

// scriptSource is the sh install script with a token, an upper-case name between "@@" pairs, in
// place of each value Render fixes. "@@" is no shell operator and appears nowhere else in the
// script, so a token is only ever one of these placeholders. Render replaces each with a shell word,
// an environment-variable identifier, the completion snippet or an install-step fragment, all
// complete as they stand, so the script never quotes a token.
//
//go:embed install.sh.in
var scriptSource string

// Each of these replaces @@INSTALL_STEP@@: extractArchive leaves the binary extracted from a tar
// archive in $binary, installRaw names the downloaded asset itself as $binary.
var (
	//go:embed extract-archive.sh.in
	extractArchive string
	//go:embed install-raw.sh.in
	installRaw string
)

// powerShellSource is the PowerShell install script, with tokens of the same form as
// scriptSource's. "@@" is no PowerShell operator either, and Render fills each token with a
// PowerShell expression, an environment-variable identifier, the completion snippet or an
// install-step fragment, each complete as it stands.
//
//go:embed install.ps1.in
var powerShellSource string

// Each of these replaces @@INSTALL_STEP@@ in powerShellSource: extractZip and extractTar leave the
// binary extracted from a zip or a tar.gz archive in $binary, installRawPowerShell names the
// downloaded asset itself as $binary.
var (
	//go:embed extract-zip.ps1.in
	extractZip string
	//go:embed extract-tar.ps1.in
	extractTar string
	//go:embed install-raw.ps1.in
	installRawPowerShell string
)

// expandsAtRuntime reports whether a placeholder is expanded when the script runs, from the shell
// variable of the same name, rather than fixed at render time as {name} and {ext} are.
func expandsAtRuntime(placeholder string) bool {
	switch placeholder {
	case "os", "arch", "tag", "version":
		return true
	default:
		return false
	}
}

// Render returns the POSIX-sh install script for the tool cfg describes. It resolves cfg with
// [update.Config.Resolved], with GOOS "linux" when cfg names none, so the script is the same on
// every host; it fails with the error of Resolved on an invalid Config, with an error wrapping
// [ErrZipAsset] when the resolved AssetTemplate holds {ext} and AssetExt is "zip", as it is by
// default for GOOS "windows", and with the error of [completioninstall.Snippet] when BinaryName is
// not a slug.
//
// Every value of cfg reaches the script as a single-quoted shell literal, so no character of an
// asset template, a binary name or a repository name is ever expanded by the shell. Each trusted
// key reaches it as its 56-character key line alone, never as the TrustedKeys entry: no comment
// line and no whitespace of an entry is ever part of the script.
func Render(cfg update.Config) ([]byte, error) {
	r, err := resolve(cfg, shGOOS)
	if err != nil {
		return nil, err
	}
	archive := asset.UsesExt(r.AssetTemplate)
	if archive && r.AssetExt == "zip" {
		return nil, fmt.Errorf("installscript: AssetExt %q: %w", r.AssetExt, ErrZipAsset)
	}
	snippet, err := completioninstall.Snippet(r.BinaryName)
	if err != nil {
		return nil, fmt.Errorf("installscript: %w", err)
	}

	step := installRaw
	if archive {
		step = extractArchive
	}
	pairs := []string{
		"@@NAME@@", shellQuote(r.BinaryName),
		"@@OWNER@@", shellQuote(url.PathEscape(r.Owner)),
		"@@REPO@@", shellQuote(url.PathEscape(r.Repo)),
		"@@ASSET@@", shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, identity),
		"@@ASSET_URL@@", shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, url.PathEscape),
		"@@CHECKSUMS@@", shellWord(r.ChecksumsAsset, r.BinaryName, "", identity),
		"@@CHECKSUMS_URL@@", shellWord(r.ChecksumsAsset, r.BinaryName, "", url.PathEscape),
		"@@INSTALL_STEP@@", strings.TrimSuffix(lf(step), "\n"),
		"@@SNIPPET@@", snippet,
		"@@TRUSTED_KEYS@@", strings.Join(trustedKeys(r, shellQuote), " "),
	}
	return fill(scriptSource, r, pairs), nil
}

// RenderPowerShell returns the PowerShell install script for the tool cfg describes, for Windows
// PowerShell 5.1 and PowerShell 7 on Windows. It resolves cfg with [update.Config.Resolved], with
// GOOS "windows" when cfg names none, so the script is the same on every host and AssetExt
// defaults to "zip"; it fails with the error of Resolved on an invalid Config and with the error
// of [completioninstall.PowerShellSnippet] when BinaryName is not a slug. A template holding {ext}
// renders the archive path for AssetExt "zip", "tar.gz" or "tgz"; a template without it renders
// the raw-binary path.
//
// Every value of cfg reaches the script as a single-quoted PowerShell literal, so no character of
// an asset template, a binary name or a repository name is ever expanded by PowerShell. Each
// trusted key reaches it as its 56-character key line alone, as in [Render].
func RenderPowerShell(cfg update.Config) ([]byte, error) {
	r, err := resolve(cfg, powerShellGOOS)
	if err != nil {
		return nil, err
	}
	snippet, err := completioninstall.PowerShellSnippet(r.BinaryName)
	if err != nil {
		return nil, fmt.Errorf("installscript: %w", err)
	}

	step := installRawPowerShell
	if asset.UsesExt(r.AssetTemplate) {
		step = extractTar
		if r.AssetExt == "zip" {
			step = extractZip
		}
	}
	pairs := []string{
		"@@NAME@@", powerShellQuote(r.BinaryName),
		"@@OWNER@@", powerShellQuote(url.PathEscape(r.Owner)),
		"@@REPO@@", powerShellQuote(url.PathEscape(r.Repo)),
		"@@ASSET@@", powerShellExpr(r.AssetTemplate, r.BinaryName, r.AssetExt, identity),
		"@@ASSET_URL@@", powerShellExpr(r.AssetTemplate, r.BinaryName, r.AssetExt, url.PathEscape),
		"@@CHECKSUMS@@", powerShellExpr(r.ChecksumsAsset, r.BinaryName, "", identity),
		"@@CHECKSUMS_URL@@", powerShellExpr(r.ChecksumsAsset, r.BinaryName, "", url.PathEscape),
		"@@INSTALL_STEP@@", strings.TrimSuffix(lf(step), "\n"),
		"@@SNIPPET@@", strings.TrimSuffix(snippet, "\n"),
		"@@TRUSTED_KEYS@@", strings.Join(trustedKeys(r, powerShellQuote), ", "),
	}
	return fill(powerShellSource, r, pairs), nil
}

// resolve returns cfg resolved by [update.Config.Resolved], with GOOS set to goos first when cfg
// names none. A renderer never reads the host's GOOS: the defaults it takes from update are those
// of the system its script runs on.
func resolve(cfg update.Config, goos string) (update.Config, error) {
	if cfg.GOOS == "" {
		cfg.GOOS = goos
	}
	r, err := cfg.Resolved()
	if err != nil {
		return update.Config{}, fmt.Errorf("installscript: %w", err)
	}
	return r, nil
}

// trustedKeys returns the key line of each of r's TrustedKeys, quoted by quote.
func trustedKeys(r update.Config, quote func(string) string) []string {
	keys := make([]string, 0, len(r.TrustedKeys))
	for _, entry := range r.TrustedKeys {
		// Resolved has validated every entry, so none fails to parse here; one that did would only
		// leave its key out of the script.
		if key, err := minisign.ParsePublicKey(entry); err == nil {
			keys = append(keys, quote(key.Line))
		}
	}
	return keys
}

// fill returns src, its line endings made LF by [lf], with its signed regions kept or dropped by
// r's signature policy and each token of pairs, and each environment-variable token, replaced.
func fill(src string, r update.Config, pairs []string) []byte {
	for _, v := range []struct{ token, suffix string }{
		{"@@VERSION_ENV@@", versionEnvSuffix},
		{"@@INSTALL_DIR_ENV@@", installDirEnvSuffix},
		{"@@BASE_URL_ENV@@", baseURLEnvSuffix},
		{"@@NO_COMPLETION_ENV@@", completioninstall.NoCompletionEnvSuffix},
		{"@@ALLOW_UNSIGNED_ENV@@", allowUnsignedEnvSuffix},
		{"@@NO_MODIFY_PATH_ENV@@", noModifyPathEnvSuffix},
	} {
		// EnvVar cannot fail once Resolved passes: the prefix is a valid identifier of at most
		// spec.MaxEnvPrefixLen (32) bytes, every suffix is a valid identifier of at most 14, and the
		// joined name stays within the 64 bytes validate.MaxEnvVarLen allows.
		name, _ := r.EnvVar(v.suffix)
		pairs = append(pairs, v.token, name)
	}

	// One pass of a Replacer never scans the text it inserts, so a token inside a value, such as
	// an asset template holding "@@NAME@@", stays literal.
	return []byte(strings.NewReplacer(pairs...).Replace(signedRegions(lf(src), !r.AllowUnsigned)))
}

// lf returns the template src with each CRLF line ending as LF, so a script renders with LF line
// endings from a checkout that converts the embedded templates to CRLF. A marker line ending in
// "\r\n" matches neither signedBegin nor signedEnd, and would leave the marker in the script.
func lf(src string) string {
	return strings.ReplaceAll(src, "\r\n", "\n")
}

// signedRegions returns src with each region from a signedBegin line to the next signedEnd line
// reduced to the lines between the two markers when keep is true, and removed whole when it is
// false. A marker is the whole of its line, so removing a region leaves src exactly as it would be
// had the region never been written.
func signedRegions(src string, keep bool) string {
	var out strings.Builder
	for {
		before, rest, found := strings.Cut(src, signedBegin)
		out.WriteString(before)
		if !found {
			return out.String()
		}
		region, after, _ := strings.Cut(rest, signedEnd)
		if keep {
			out.WriteString(region)
		}
		src = after
	}
}

// identity returns s unchanged.
func identity(s string) string { return s }

// templatePart is one run of a rendered asset template: literal text, or the name of a placeholder
// expanded when the script runs.
type templatePart struct {
	text    string
	runtime bool
}

// templateParts returns the asset template tmpl rendered with name for {name} and ext for {ext}, as
// alternating runs: each run of literal text, name and ext included, goes through enc, and each
// placeholder [expandsAtRuntime] accepts is a run of its own. tmpl is a template a resolved Config
// carries, so every "{" opens a known placeholder that a "}" closes.
func templateParts(tmpl, name, ext string, enc func(string) string) []templatePart {
	var parts []templatePart
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			parts = append(parts, templatePart{text: enc(literal.String())})
			literal.Reset()
		}
	}
	rest := tmpl
	for rest != "" {
		text, afterOpen, found := strings.Cut(rest, "{")
		literal.WriteString(text)
		if !found {
			break
		}
		placeholder, after, _ := strings.Cut(afterOpen, "}")
		rest = after
		if expandsAtRuntime(placeholder) {
			flush()
			parts = append(parts, templatePart{text: placeholder, runtime: true})
			continue
		}
		if placeholder == "name" {
			literal.WriteString(name)
		} else {
			literal.WriteString(ext)
		}
	}
	flush()
	return parts
}

// shellWord returns one shell word that evaluates to the asset template tmpl rendered as
// [templateParts] renders it: each literal run single-quoted, each run-time placeholder
// "${placeholder}".
func shellWord(tmpl, name, ext string, enc func(string) string) string {
	var word strings.Builder
	for _, p := range templateParts(tmpl, name, ext, enc) {
		if p.runtime {
			word.WriteString(`"${` + p.text + `}"`)
		} else {
			word.WriteString(shellQuote(p.text))
		}
	}
	return word.String()
}

// powerShellExpr returns one PowerShell expression that evaluates to the asset template tmpl
// rendered as [templateParts] renders it: the runs joined by "+", each literal run single-quoted
// and each run-time placeholder the variable $placeholder, which the script sets to a string.
func powerShellExpr(tmpl, name, ext string, enc func(string) string) string {
	parts := templateParts(tmpl, name, ext, enc)
	terms := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.runtime {
			terms = append(terms, "$"+p.text)
		} else {
			terms = append(terms, powerShellQuote(p.text))
		}
	}
	return "(" + strings.Join(terms, " + ") + ")"
}

// shellQuote returns s as a single-quoted shell literal. Each single quote in s closes the quoting,
// is written backslash-escaped and reopens it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// powerShellQuote returns s as a single-quoted PowerShell literal. PowerShell takes each of the
// four typographic single quotes U+2018 to U+201B for "'", so every one of the five quotes in s is
// doubled, which is how a single-quoted string holds one.
func powerShellQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, c := range s {
		b.WriteRune(c)
		switch c {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
