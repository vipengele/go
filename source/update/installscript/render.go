package installscript

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/asset"
)

// Suffixes of the environment variables the script reads, each joined to the EnvPrefix by
// [spec.Identity.EnvVar].
const (
	versionEnvSuffix    = "VERSION"
	installDirEnvSuffix = "INSTALL_DIR"
	baseURLEnvSuffix    = "BASE_URL"
)

// scriptTemplate is the text/template source of the install script. Every value it receives is
// already a shell word or an environment-variable identifier, so the template never quotes.
//
//go:embed install.sh.tmpl
var scriptTemplate string

// runtimeVar returns the shell variable holding the value of a placeholder expanded when the
// script runs. It reports false for {name} and {ext}, which are fixed at render time.
func runtimeVar(placeholder string) (string, bool) {
	switch placeholder {
	case "os", "arch", "tag", "version":
		return placeholder, true
	default:
		return "", false
	}
}

// scriptData is what the template is executed with.
type scriptData struct {
	// Name is the binary name as a shell word.
	Name string
	// Owner and Repo are the repository's path segments, URL-escaped, as shell words.
	Owner string
	Repo  string
	// Archive selects the tar extraction path; false installs the asset as the binary itself.
	Archive bool
	// Asset and AssetURL are shell words evaluating to the asset's name and to that name escaped
	// as a URL path segment.
	Asset    string
	AssetURL string
	// Checksums and ChecksumsURL are the same two words for the checksums file.
	Checksums    string
	ChecksumsURL string
	// The environment variables the script reads, as identifiers.
	VersionEnv      string
	InstallDirEnv   string
	BaseURLEnv      string
	NoCompletionEnv string
	// Snippet is the completion snippet, inserted verbatim.
	Snippet string
}

// Render returns the install script for the tool cfg describes. It resolves cfg with
// [update.Config.Resolved] and fails with its error on an invalid Config; it fails with an error
// wrapping [ErrZipAsset] when the resolved AssetTemplate holds {ext} and AssetExt is "zip", and
// with the error of [completioninstall.Snippet] when BinaryName is not a slug.
//
// Every value of cfg reaches the script as a single-quoted shell literal, so no character of an
// asset template, a binary name or a repository name is ever expanded by the shell.
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

	data := scriptData{
		Name:     shellQuote(r.BinaryName),
		Owner:    shellQuote(url.PathEscape(r.Owner)),
		Repo:     shellQuote(url.PathEscape(r.Repo)),
		Archive:  archive,
		Asset:    shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, identity),
		AssetURL: shellWord(r.AssetTemplate, r.BinaryName, r.AssetExt, url.PathEscape),
		Snippet:  snippet,
	}
	data.Checksums = shellWord(r.ChecksumsAsset, r.BinaryName, "", identity)
	data.ChecksumsURL = shellWord(r.ChecksumsAsset, r.BinaryName, "", url.PathEscape)
	for _, v := range []struct {
		dst    *string
		suffix string
	}{
		{&data.VersionEnv, versionEnvSuffix},
		{&data.InstallDirEnv, installDirEnvSuffix},
		{&data.BaseURLEnv, baseURLEnvSuffix},
		{&data.NoCompletionEnv, completioninstall.NoCompletionEnvSuffix},
	} {
		if *v.dst, err = r.EnvVar(v.suffix); err != nil {
			return nil, fmt.Errorf("installscript: %w", err)
		}
	}

	tmpl, err := template.New("install.sh").Option("missingkey=error").Parse(scriptTemplate)
	if err != nil {
		return nil, fmt.Errorf("installscript: parsing the script template: %w", err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("installscript: rendering the script: %w", err)
	}
	return b.Bytes(), nil
}

// identity returns s unchanged.
func identity(s string) string { return s }

// shellWord returns one shell word that evaluates to the asset template tmpl rendered with name for
// {name}, ext for {ext} and, at run time, the shell variables of [runtimeVar] for the others. Each
// run of literal text, name and ext included, goes through enc and is single-quoted; each run-time
// placeholder becomes "${var}". tmpl is a template a resolved Config carries, so every "{" opens a
// known placeholder that a "}" closes.
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
		if v, ok := runtimeVar(placeholder); ok {
			flush()
			word.WriteString(`"${` + v + `}"`)
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
