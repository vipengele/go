package installscript_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/installscript"
	"go.vipengele.org/update/internal/minisigntest"
)

// testConfig returns a valid Config with an explicit StateDir, so resolving it consults no
// environment, and with the asset template and extension given.
func testConfig(assetTemplate, assetExt string) update.Config {
	return update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "pedromvgomes",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		TrustedKeys:   []string{testKeyLine()},
		StateDir:      "/state",
		AssetTemplate: assetTemplate,
		AssetExt:      assetExt,
	}
}

// testKeyLine returns the key line of the minisign key GenerateKey draws from a fixed stream, the
// same line on every call. The stream is longer than the key id and Ed25519 seed GenerateKey
// reads, so it never runs dry.
func testKeyLine() string {
	key, err := minisigntest.GenerateKey(strings.NewReader(strings.Repeat("render test key ", 4)))
	if err != nil {
		panic(err)
	}
	return key.PublicKeyLine()
}

// render renders cfg and fails the test on an error.
func render(t *testing.T, cfg update.Config) string {
	t.Helper()
	out, err := installscript.Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return string(out)
}

// renderPowerShell renders the PowerShell script for cfg and fails the test on an error.
func renderPowerShell(t *testing.T, cfg update.Config) string {
	t.Helper()
	out, err := installscript.RenderPowerShell(cfg)
	if err != nil {
		t.Fatalf("RenderPowerShell: %v", err)
	}
	return string(out)
}

// shPath returns the sh the script is checked with, skipping the test when there is none or the
// host is Windows. The script installs on Linux and macOS only and renders to the same bytes on
// every host, so those hosts check it; the sh a Windows host has is an emulation whose path
// translation the script never meets.
func shPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the install script runs on Linux and macOS only, and those hosts check it")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	return sh
}

func TestRenderInvalidConfig(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	cfg.Name = ""
	if _, err := installscript.Render(cfg); err == nil {
		t.Fatal("Render with an empty Name succeeded")
	}
}

func TestRenderRejectsZipArchive(t *testing.T) {
	t.Parallel()
	_, err := installscript.Render(testConfig("{name}_{os}_{arch}.{ext}", "zip"))
	if !errors.Is(err, installscript.ErrZipAsset) {
		t.Fatalf("Render error = %v, want it to match ErrZipAsset", err)
	}
	if n := strings.Count(err.Error(), "installscript"); n != 1 {
		t.Fatalf("Render error %q names installscript %d times, want once", err, n)
	}
}

func TestRenderRejectsBinaryNameThatIsNotASlug(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	cfg.BinaryName = "My Tool"
	out, err := installscript.Render(cfg)
	if err == nil {
		t.Fatal("Render with a BinaryName that is not a slug succeeded")
	}
	if out != nil {
		t.Fatalf("Render returned a script alongside the error: %q", out)
	}
}

func TestRenderArchiveExtractsTar(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{"tgz", "tar.gz"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()
			out := render(t, testConfig("{name}_{os}_{arch}.{ext}", ext))
			if !strings.Contains(out, "tar -xzf") {
				t.Error("script does not extract a tar archive")
			}
			if !strings.Contains(out, "."+ext) {
				t.Errorf("script does not name the .%s asset", ext)
			}
		})
	}
}

func TestRenderRawBinaryInstallsAssetItself(t *testing.T) {
	t.Parallel()
	out := render(t, testConfig("{name}_{os}_{arch}", ""))
	if strings.Contains(out, "tar -xzf") {
		t.Error("script extracts a tar archive for a template without {ext}")
	}
	if !strings.Contains(out, "binary=$tmp/asset\n") {
		t.Error("script does not install the asset as the binary")
	}
}

func TestRenderCarriesCompletionAndEnvNames(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	out := render(t, cfg)

	snippet, err := completioninstall.Snippet("atk")
	if err != nil {
		t.Fatalf("Snippet: %v", err)
	}
	if !strings.Contains(out, snippet) {
		t.Error("script does not contain the completion snippet")
	}
	for _, suffix := range []string{"VERSION", "INSTALL_DIR", "BASE_URL", completioninstall.NoCompletionEnvSuffix} {
		name, err := cfg.EnvVar(suffix)
		if err != nil {
			t.Fatalf("EnvVar(%q): %v", suffix, err)
		}
		if !strings.Contains(out, name) {
			t.Errorf("script does not mention %s", name)
		}
	}
}

// hostileTemplate holds every character a shell treats specially inside a word, around the
// placeholders that are expanded when the script runs.
const hostileTemplate = "it's-$HOME-`id`-\"q\"-\\-{name}-{os}-{arch}-{tag}-{version}"

// hostileAsset is hostileTemplate rendered for the values the test script runs with.
const hostileAsset = "it's-$HOME-`id`-\"q\"-\\-atk-linux-amd64-v1.2.3-1.2.3"

func TestRenderHostileAssetTemplateStaysLiteral(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext, want string
	}{
		{"raw", hostileTemplate, "", hostileAsset},
		{"archive", hostileTemplate + ".{ext}", "tgz", hostileAsset + ".tgz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sh := shPath(t)
			out := render(t, testConfig(tt.tmpl, tt.ext))

			syntax := exec.Command(sh, "-n")
			syntax.Stdin = strings.NewReader(out)
			if msg, err := syntax.CombinedOutput(); err != nil {
				t.Fatalf("sh -n rejects the script: %v\n%s", err, msg)
			}

			var line string
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "asset=") {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatal("script has no asset= line")
			}
			script := "os=linux arch=amd64 tag=v1.2.3 version=1.2.3\n" + line + "\nprintf %s \"$asset\""
			got, err := exec.Command(sh, "-c", script).Output()
			if err != nil {
				t.Fatalf("running %q: %v", line, err)
			}
			if string(got) != tt.want {
				t.Errorf("asset = %q, want %q", got, tt.want)
			}
		})
	}
}

// scriptLine returns the first line of out that starts with prefix.
func scriptLine(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	t.Fatalf("script has no %s line", prefix)
	return ""
}

func TestRenderNamesAssetsWithoutEmptyLiterals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
		want            map[string]string
	}{
		{"default", "", "", map[string]string{
			"asset=":         `asset='atk_'"${os}"'_'"${arch}"'.tar.gz'`,
			"asset_url=":     `asset_url=$base/$owner/$repo/releases/download/$tag/'atk_'"${os}"'_'"${arch}"'.tar.gz'`,
			"checksums=":     `checksums='checksums.txt'`,
			"checksums_url=": `checksums_url=$base/$owner/$repo/releases/download/$tag/'checksums.txt'`,
		}},
		{"leading placeholder", "{os}-{name}", "", map[string]string{
			"asset=": `asset="${os}"'-atk'`,
		}},
		{"trailing placeholder", "{name}-{os}", "", map[string]string{
			"asset=": `asset='atk-'"${os}"`,
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := render(t, testConfig(tt.tmpl, tt.ext))
			for prefix, want := range tt.want {
				if got := scriptLine(t, out, prefix); got != want {
					t.Errorf("%s line = %q, want %q", prefix, got, want)
				}
			}
		})
	}
}

func TestRenderFillsEveryToken(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
	}{
		{"archive", "{name}_{os}_{arch}.{ext}", "tar.gz"},
		{"raw", "{name}_{os}_{arch}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := render(t, testConfig(tt.tmpl, tt.ext))
			if i := strings.Index(out, "@@"); i >= 0 {
				line := out[strings.LastIndex(out[:i], "\n")+1:]
				line, _, _ = strings.Cut(line, "\n")
				t.Errorf("script keeps an unfilled token: %q", line)
			}
		})
	}
}

func TestRenderLeavesTokenInValueLiteral(t *testing.T) {
	t.Parallel()
	out := render(t, testConfig("@@SNIPPET@@-@@NAME@@-{name}", ""))
	want := `asset='@@SNIPPET@@-@@NAME@@-atk'`
	if got := scriptLine(t, out, "asset="); got != want {
		t.Errorf("asset= line = %q, want %q", got, want)
	}
}

// unsignedConfig returns testConfig(assetTemplate, assetExt) for a tool whose releases are
// unsigned.
func unsignedConfig(assetTemplate, assetExt string) update.Config {
	cfg := testConfig(assetTemplate, assetExt)
	cfg.TrustedKeys, cfg.AllowUnsigned = nil, true
	return cfg
}

func TestRenderRequiresASignaturePolicy(t *testing.T) {
	t.Parallel()
	neither := testConfig("", "")
	neither.TrustedKeys = nil
	both := testConfig("", "")
	both.AllowUnsigned = true
	for name, cfg := range map[string]update.Config{"neither": neither, "both": both} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := installscript.Render(cfg)
			if err == nil {
				t.Fatal("Render succeeded")
			}
			if out != nil {
				t.Fatalf("Render returned a script alongside the error: %q", out)
			}
		})
	}
}

func TestRenderEmbedsEachTrustedKeyAsItsKeyLine(t *testing.T) {
	t.Parallel()
	sh := shPath(t)
	other := generateKey(t)
	cfg := testConfig("", "")
	cfg.TrustedKeys = []string{
		" " + cfg.TrustedKeys[0] + "\n",
		"untrusted comment: minisign public key\r\n" + other.PublicKeyLine() + "\n",
	}
	out := render(t, cfg)

	want := "\tfor key in '" + strings.TrimSpace(testConfig("", "").TrustedKeys[0]) + "' '" + other.PublicKeyLine() + "'; do"
	if got := scriptLine(t, out, "\tfor key in "); got != want {
		t.Errorf("key loop = %q, want %q", got, want)
	}
	if strings.Contains(out, "untrusted comment") {
		t.Error("the script carries an entry's comment line")
	}
	allow, err := cfg.EnvVar("ALLOW_UNSIGNED")
	if err != nil {
		t.Fatalf("EnvVar: %v", err)
	}
	if !strings.Contains(out, "${"+allow+":-}") {
		t.Errorf("the script does not read %s", allow)
	}
	syntax := exec.Command(sh, "-n")
	syntax.Stdin = strings.NewReader(out)
	if msg, err := syntax.CombinedOutput(); err != nil {
		t.Fatalf("sh -n rejects the script: %v\n%s", err, msg)
	}
}

func TestRenderUnsignedOmitsTheSignatureCheck(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
	}{
		{"default", "", ""},
		{"archive", "{name}_{os}_{arch}.{ext}", "tgz"},
		{"raw", "{name}_{os}_{arch}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := unsignedConfig(tt.tmpl, tt.ext)
			unsigned := render(t, cfg)
			signed := render(t, testConfig(tt.tmpl, tt.ext))

			allow, err := cfg.EnvVar("ALLOW_UNSIGNED")
			if err != nil {
				t.Fatalf("EnvVar: %v", err)
			}
			for _, absent := range []string{"minisig", allow, "@@"} {
				if strings.Contains(unsigned, absent) {
					t.Errorf("the unsigned script mentions %q", absent)
				}
			}
			// The signature check only adds lines: dropping them from the signed script leaves
			// the unsigned one, line for line.
			rest := strings.SplitAfter(unsigned, "\n")
			added := 0
			for _, line := range strings.SplitAfter(signed, "\n") {
				if len(rest) > 0 && line == rest[0] {
					rest = rest[1:]
					continue
				}
				added++
			}
			if len(rest) != 0 {
				t.Errorf("the signed script lacks the unsigned script's line %q and %d after it", rest[0], len(rest)-1)
			}
			if added == 0 {
				t.Error("the signed script adds no line to the unsigned one")
			}
		})
	}
}

func TestRenderFillsACRLFTemplateWithLFLineEndings(t *testing.T) {
	t.Parallel()
	src := "a\r\n@@SIGNED_BEGIN@@\r\ncheck\r\n@@SIGNED_END@@\r\nb @@VERSION_ENV@@\r\n"
	for _, tt := range []struct {
		name string
		cfg  update.Config
		want string
	}{
		{"signed", testConfig("", ""), "a\ncheck\nb ATK_VERSION\n"},
		{"unsigned", unsignedConfig("", ""), "a\nb ATK_VERSION\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := installscript.FillTemplate(src, tt.cfg)
			if err != nil {
				t.Fatalf("FillTemplate: %v", err)
			}
			if string(out) != tt.want {
				t.Errorf("FillTemplate = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestRenderResolvesForAPOSIXSystemWhateverTheHost(t *testing.T) {
	t.Parallel()
	for _, tmpl := range []string{"", "{name}_{os}_{arch}"} {
		cfg := testConfig(tmpl, "")
		want := render(t, cfg)
		for _, goos := range []string{"linux", "darwin", "freebsd"} {
			cfg.GOOS = goos
			if got := render(t, cfg); got != want {
				t.Errorf("template %q: the script for GOOS %q differs from the one for an unset GOOS", tmpl, goos)
			}
		}
	}
	out := render(t, testConfig("", ""))
	if !strings.Contains(out, ".tar.gz'") || strings.Contains(out, ".zip") {
		t.Errorf("the script for an unset GOOS does not name the tar.gz asset:\n%s", scriptLine(t, out, "asset="))
	}
}

func TestRenderForWindowsGOOSRejectsItsDefaultZip(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	cfg.GOOS = "windows"
	if _, err := installscript.Render(cfg); !errors.Is(err, installscript.ErrZipAsset) {
		t.Fatalf("Render error = %v, want it to match ErrZipAsset", err)
	}
	cfg.AssetExt = "tar.gz"
	render(t, cfg)
}

// powerShellLine returns the first line of out that, once its indentation is trimmed, starts with
// prefix.
func powerShellLine(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimLeft(l, "\t"); strings.HasPrefix(l, prefix) {
			return l
		}
	}
	t.Fatalf("script has no %s line", prefix)
	return ""
}

func TestRenderPowerShellPicksTheInstallStepForTheAsset(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext, goos string
		wantAsset, wantStep   string
		absent                []string
	}{
		{
			name: "zip by default", wantAsset: `$asset = ('atk_' + $os + '_' + $arch + '.zip')`,
			wantStep: "Expand-Archive -LiteralPath $zip -DestinationPath $extract", absent: []string{"tar.exe"},
		},
		{
			name: "zip for GOOS windows", goos: "windows", wantAsset: `$asset = ('atk_' + $os + '_' + $arch + '.zip')`,
			wantStep: "Expand-Archive -LiteralPath $zip -DestinationPath $extract", absent: []string{"tar.exe"},
		},
		{
			name: "tar.gz", tmpl: "{name}_{os}_{arch}.{ext}", ext: "tar.gz",
			wantAsset: `$asset = ('atk_' + $os + '_' + $arch + '.tar.gz')`,
			wantStep:  "$run = InvokeTool $tar @('-xzf', $assetFile, '-C', $extract)", absent: []string{"Expand-Archive"},
		},
		{
			name: "tgz", tmpl: "{name}_{os}_{arch}.{ext}", ext: "tgz",
			wantAsset: `$asset = ('atk_' + $os + '_' + $arch + '.tgz')`,
			wantStep:  "$run = InvokeTool $tar @('-xzf', $assetFile, '-C', $extract)", absent: []string{"Expand-Archive"},
		},
		{
			name: "tar.gz for another GOOS", tmpl: "{name}_{os}_{arch}.{ext}", goos: "linux",
			wantAsset: `$asset = ('atk_' + $os + '_' + $arch + '.tar.gz')`,
			wantStep:  "$run = InvokeTool $tar @('-xzf', $assetFile, '-C', $extract)", absent: []string{"Expand-Archive"},
		},
		{
			name: "raw", tmpl: "{name}_{os}_{arch}", ext: "zip",
			wantAsset: `$asset = ('atk_' + $os + '_' + $arch)`,
			wantStep:  "$binary = $assetFile", absent: []string{"Expand-Archive", "tar.exe", "FindBinary $extract"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(tt.tmpl, tt.ext)
			cfg.GOOS = tt.goos
			out := renderPowerShell(t, cfg)
			if got := powerShellLine(t, out, "$asset = "); got != tt.wantAsset {
				t.Errorf("asset line = %q, want %q", got, tt.wantAsset)
			}
			if !strings.Contains(out, "\t"+tt.wantStep+"\n") {
				t.Errorf("the script lacks the install step line %q", tt.wantStep)
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("the script mentions %q", a)
				}
			}
			checkPowerShellSyntax(t, out)
		})
	}
}

func TestRenderPowerShellIsTheSameWhateverTheHost(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	unset := renderPowerShell(t, cfg)
	cfg.GOOS = "windows"
	if renderPowerShell(t, cfg) != unset {
		t.Error("the script for GOOS windows differs from the one for an unset GOOS")
	}
}

func TestRenderPowerShellFillsEveryToken(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
		unsigned        bool
	}{
		{name: "zip", tmpl: "{name}_{os}_{arch}.{ext}", ext: "zip"},
		{name: "tar.gz", tmpl: "{name}_{os}_{arch}.{ext}", ext: "tar.gz"},
		{name: "raw", tmpl: "{name}_{os}_{arch}"},
		{name: "unsigned", unsigned: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(tt.tmpl, tt.ext)
			if tt.unsigned {
				cfg = unsignedConfig(tt.tmpl, tt.ext)
			}
			out := renderPowerShell(t, cfg)
			if i := strings.Index(out, "@@"); i >= 0 {
				line := out[strings.LastIndex(out[:i], "\n")+1:]
				line, _, _ = strings.Cut(line, "\n")
				t.Errorf("script keeps an unfilled token: %q", line)
			}
		})
	}
}

func TestRenderPowerShellCarriesEnvNames(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	out := renderPowerShell(t, cfg)
	for _, suffix := range []string{
		"VERSION", "INSTALL_DIR", "BASE_URL", "ALLOW_UNSIGNED", "NO_MODIFY_PATH",
		completioninstall.NoCompletionEnvSuffix,
	} {
		name, err := cfg.EnvVar(suffix)
		if err != nil {
			t.Fatalf("EnvVar(%q): %v", suffix, err)
		}
		if !strings.Contains(out, "$env:"+name+" ") && !strings.Contains(out, "$env:"+name+"\n") {
			t.Errorf("script does not read $env:%s", name)
		}
	}
}

func TestRenderPowerShellEmbedsTheGuardedCompletionSnippet(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	out := renderPowerShell(t, cfg)
	snippet, err := completioninstall.PowerShellSnippet("atk")
	if err != nil {
		t.Fatalf("PowerShellSnippet: %v", err)
	}
	noCompletion, err := cfg.EnvVar(completioninstall.NoCompletionEnvSuffix)
	if err != nil {
		t.Fatalf("EnvVar: %v", err)
	}
	guard := "\t\t$" + completioninstall.PowerShellBinaryVar + " = $target\n" +
		"\t\tif ($env:" + noCompletion + " -ne '1') {\n" + snippet + "\t\t}\n"
	if !strings.Contains(out, guard) {
		t.Errorf("the script does not run the completion snippet under the %s guard", noCompletion)
	}
}

func TestRenderPowerShellRejectsBinaryNameThatIsNotASlug(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	cfg.BinaryName = "My Tool"
	out, err := installscript.RenderPowerShell(cfg)
	if err == nil {
		t.Fatal("RenderPowerShell with a BinaryName that is not a slug succeeded")
	}
	if out != nil {
		t.Fatalf("RenderPowerShell returned a script alongside the error: %q", out)
	}
}

func TestRenderPowerShellRequiresASignaturePolicy(t *testing.T) {
	t.Parallel()
	cfg := testConfig("", "")
	cfg.TrustedKeys = nil
	if out, err := installscript.RenderPowerShell(cfg); err == nil || out != nil {
		t.Fatalf("RenderPowerShell = %q, %v; want no script and an error", out, err)
	}
}

func TestRenderPowerShellEmbedsEachTrustedKeyAsItsKeyLine(t *testing.T) {
	t.Parallel()
	other := generateKey(t)
	cfg := testConfig("", "")
	cfg.TrustedKeys = []string{
		" " + cfg.TrustedKeys[0] + "\n",
		"untrusted comment: minisign public key\r\n" + other.PublicKeyLine() + "\n",
	}
	out := renderPowerShell(t, cfg)
	want := "foreach ($key in @('" + strings.TrimSpace(testConfig("", "").TrustedKeys[0]) + "', '" + other.PublicKeyLine() + "')) {"
	if got := powerShellLine(t, out, "foreach ($key in "); got != want {
		t.Errorf("key loop = %q, want %q", got, want)
	}
	if strings.Contains(out, "untrusted comment") {
		t.Error("the script carries an entry's comment line")
	}
	checkPowerShellSyntax(t, out)
}

func TestRenderPowerShellUnsignedOmitsTheSignatureCheck(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, tmpl, ext string
	}{
		{"default", "", ""},
		{"tar.gz", "{name}_{os}_{arch}.{ext}", "tar.gz"},
		{"raw", "{name}_{os}_{arch}", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := unsignedConfig(tt.tmpl, tt.ext)
			unsigned := renderPowerShell(t, cfg)
			signed := renderPowerShell(t, testConfig(tt.tmpl, tt.ext))

			allow, err := cfg.EnvVar("ALLOW_UNSIGNED")
			if err != nil {
				t.Fatalf("EnvVar: %v", err)
			}
			for _, absent := range []string{"minisig", allow, "@@", "TestInteractive)"} {
				if strings.Contains(unsigned, absent) {
					t.Errorf("the unsigned script mentions %q", absent)
				}
			}
			if !strings.Contains(signed, "minisign") {
				t.Error("the signed script does not mention minisign")
			}
			// The signature check only adds lines: dropping them from the signed script leaves
			// the unsigned one, line for line.
			rest := strings.SplitAfter(unsigned, "\n")
			added := 0
			for _, line := range strings.SplitAfter(signed, "\n") {
				if len(rest) > 0 && line == rest[0] {
					rest = rest[1:]
					continue
				}
				added++
			}
			if len(rest) != 0 {
				t.Errorf("the signed script lacks the unsigned script's line %q and %d after it", rest[0], len(rest)-1)
			}
			if added == 0 {
				t.Error("the signed script adds no line to the unsigned one")
			}
			checkPowerShellSyntax(t, unsigned)
		})
	}
}

// hostilePowerShellTemplate holds every character PowerShell treats specially inside a string,
// the typographic single quotes included, around the placeholders that are expanded when the
// script runs.
const hostilePowerShellTemplate = "it's-\u2018\u2019\u201a\u201b-$HOME-$(id)-`n-\"q\"-@@NAME@@-{name}-{os}-{arch}-{tag}-{version}"

func TestRenderPowerShellHostileAssetTemplateStaysLiteral(t *testing.T) {
	t.Parallel()
	pwsh := pwshPath(t)
	out := renderPowerShell(t, testConfig(hostilePowerShellTemplate+".{ext}", "zip"))
	checkPowerShellSyntax(t, out)

	line := powerShellLine(t, out, "$asset = ")
	script := "$os = 'windows'; $arch = 'amd64'; $tag = 'v1.2.3'; $version = '1.2.3'\n" + line +
		"\n[Console]::Out.Write($asset)\n"
	path := filepath.Join(t.TempDir(), "asset.ps1")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing the script: %v", err)
	}
	got, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", path).Output()
	if err != nil {
		t.Fatalf("running %q: %v", line, err)
	}
	want := "it's-\u2018\u2019\u201a\u201b-$HOME-$(id)-`n-\"q\"-@@NAME@@-atk-windows-amd64-v1.2.3-1.2.3.zip"
	if string(got) != want {
		t.Errorf("asset = %q, want %q", got, want)
	}
}

// pwshPath returns the PowerShell 7 the script is parsed with, skipping the test when pwsh is not
// on PATH.
func pwshPath(t *testing.T) string {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not on PATH")
	}
	return pwsh
}

// checkPowerShellSyntax fails the test when PowerShell's parser reports an error in script. It
// checks nothing when pwsh is not on PATH.
func checkPowerShellSyntax(t *testing.T, script string) {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "install.ps1")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing the script: %v", err)
	}
	check := "$errors = $null\n" +
		"[void][System.Management.Automation.Language.Parser]::ParseFile($args[0], [ref]$null, [ref]$errors)\n" +
		"foreach ($e in $errors) { [Console]::Out.WriteLine($e.Extent.StartLineNumber.ToString() + ': ' + $e.Message) }\n" +
		"if ($errors.Count) { exit 1 }\n"
	checker := filepath.Join(dir, "check.ps1")
	if err := os.WriteFile(checker, []byte(check), 0o644); err != nil {
		t.Fatalf("writing the checker: %v", err)
	}
	if msg, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", checker, path).CombinedOutput(); err != nil {
		t.Fatalf("PowerShell's parser rejects the script: %v\n%s", err, msg)
	}
}

func TestRenderPowerShellDoublesEverySingleQuoteInTheAssetLiteral(t *testing.T) {
	t.Parallel()
	out := renderPowerShell(t, testConfig("a'b‘c’d‚e‛f-{name}", "zip"))

	line := powerShellLine(t, out, "$asset = ")
	for _, want := range []string{"a''b", "‘‘c", "’’d", "‚‚e", "‛‛f"} {
		if !strings.Contains(line, want) {
			t.Errorf("asset line %q lacks the doubled quote %q", line, want)
		}
	}
}
