package installscript_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/installscript"
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
		StateDir:      "/state",
		AssetTemplate: assetTemplate,
		AssetExt:      assetExt,
	}
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

// shPath returns the sh the script is checked with, skipping the test when there is none.
func shPath(t *testing.T) string {
	t.Helper()
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
