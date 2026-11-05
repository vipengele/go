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

// Suffixes of the environment variables the script reads, each joined to the EnvPrefix by
// [spec.Identity.EnvVar].
const (
	versionEnvSuffix    = "VERSION"
	installDirEnvSuffix = "INSTALL_DIR"
	baseURLEnvSuffix    = "BASE_URL"
	// allowUnsignedEnvSuffix names the variable that, set to 1, lets a script rendered with
	// trusted keys install on the checksum alone when minisign is not installed.
	allowUnsignedEnvSuffix = "ALLOW_UNSIGNED"
)

// The lines from signedBegin to signedEnd, both marker lines included, are the signature check:
// Render keeps what lies between them for a Config with TrustedKeys and drops it all under
// AllowUnsigned, so a script for unsigned releases holds no trace of the check.
const (
	signedBegin = "@@SIGNED_BEGIN@@\n"
	signedEnd   = "@@SIGNED_END@@\n"
)

// scriptSource is the install script with a token, an upper-case name between "@@" pairs, in
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

// Render returns the install script for the tool cfg describes. It resolves cfg with
// [update.Config.Resolved] and fails with its error on an invalid Config; it fails with an error
// wrapping [ErrZipAsset] when the resolved AssetTemplate holds {ext} and AssetExt is "zip", and
// with the error of [completioninstall.Snippet] when BinaryName is not a slug.
//
// Every value of cfg reaches the script as a single-quoted shell literal, so no character of an
// asset template, a binary name or a repository name is ever expanded by the shell. Each trusted
// key reaches it as its 56-character key line alone, never as the TrustedKeys entry: no comment
// line and no whitespace of an entry is ever part of the script.
func Render(cfg update.Config) ([]byte, error) {
	r, err := cfg.Resolved()
	if err != nil {
		return nil, fmt.Errorf("installscript: %w", err)
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
	keys := make([]string, 0, len(r.TrustedKeys))
	for _, entry := range r.TrustedKeys {
		// Resolved has already parsed every entry the same way, in Validate.
		key, err := minisign.ParsePublicKey(entry)
		if err != nil {
			return nil, fmt.Errorf("installscript: %w", err)
		}
		keys = append(keys, shellQuote(key.Line))
	}
	pairs := []string{
		"@@NAME@@", shellQuote(r.BinaryName),
		"@@OWNER@@", shellQuote(url.PathEscape(r.Owner)),
		"@@REPO@@", shellQuote(url.PathEscape(r.Repo)),
		"@@ASSET@@", shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, identity),
		"@@ASSET_URL@@", shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, url.PathEscape),
		"@@CHECKSUMS@@", shellWord(r.ChecksumsAsset, r.BinaryName, "", identity),
		"@@CHECKSUMS_URL@@", shellWord(r.ChecksumsAsset, r.BinaryName, "", url.PathEscape),
		"@@INSTALL_STEP@@", strings.TrimSuffix(step, "\n"),
		"@@SNIPPET@@", snippet,
		"@@TRUSTED_KEYS@@", strings.Join(keys, " "),
	}
	for _, v := range []struct{ token, suffix string }{
		{"@@VERSION_ENV@@", versionEnvSuffix},
		{"@@INSTALL_DIR_ENV@@", installDirEnvSuffix},
		{"@@BASE_URL_ENV@@", baseURLEnvSuffix},
		{"@@NO_COMPLETION_ENV@@", completioninstall.NoCompletionEnvSuffix},
		{"@@ALLOW_UNSIGNED_ENV@@", allowUnsignedEnvSuffix},
	} {
		// EnvVar cannot fail once Resolved passes: the prefix is a valid identifier of at most
		// spec.MaxEnvPrefixLen (32) bytes, every suffix is a valid identifier of at most 14, and the
		// joined name stays within the 64 bytes validate.MaxEnvVarLen allows.
		name, _ := r.EnvVar(v.suffix)
		pairs = append(pairs, v.token, name)
	}

	// One pass of a Replacer never scans the text it inserts, so a token inside a value, such as
	// an asset template holding "@@NAME@@", stays literal.
	return []byte(strings.NewReplacer(pairs...).Replace(signedRegions(scriptSource, !r.AllowUnsigned))), nil
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

// shellWord returns one shell word that evaluates to the asset template tmpl rendered with name for
// {name}, ext for {ext} and, at run time, the shell variable of the same name for each placeholder
// [expandsAtRuntime] accepts. Each run of literal text, name and ext included, goes through enc and
// is single-quoted; each run-time placeholder becomes "${placeholder}". tmpl is a template a
// resolved Config carries, so every "{" opens a known placeholder that a "}" closes.
func shellWord(tmpl, name, ext string, enc func(string) string) string {
	var word, literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			word.WriteString(shellQuote(enc(literal.String())))
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
			word.WriteString(`"${` + placeholder + `}"`)
			continue
		}
		if placeholder == "name" {
			literal.WriteString(name)
		} else {
			literal.WriteString(ext)
		}
	}
	flush()
	return word.String()
}

// shellQuote returns s as a single-quoted shell literal. Each single quote in s closes the quoting,
// is written backslash-escaped and reopens it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
