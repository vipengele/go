package completioninstall_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/commons/validate"
)

func TestPowerShellSnippetNamesTheTool(t *testing.T) {
	for _, name := range []string{"atk", "my-tool", "a1-b2-c3"} {
		t.Run(name, func(t *testing.T) {
			s, err := completioninstall.PowerShellSnippet(name)
			if err != nil {
				t.Fatalf("PowerShellSnippet(%q): %v", name, err)
			}
			for _, want := range []string{
				"'" + name + ".ps1'",
				"& $CompletionBinary completion powershell",
				"'Completions'",
				"'Documents', 'PowerShell'",
				"'.config', 'powershell'",
				"Write-Warning",
				"New-Item -ItemType Directory",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("snippet lacks %q", want)
				}
			}
			if strings.Contains(s, "_NO_COMPLETION") {
				t.Error("snippet guards itself; the embedding script owns the opt-out")
			}
			if strings.Contains(s, "@") {
				t.Error("snippet holds an unreplaced placeholder")
			}
		})
	}
}

func TestPowerShellSnippetPrintsTheProfileLineAndNeverEditsTheProfile(t *testing.T) {
	s, err := completioninstall.PowerShellSnippet("atk")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `Write-Host ('. ''' + $ciFile.Replace("'", "''") + '''')`) {
		t.Error("snippet does not print the dot-source line")
	}
	for _, line := range strings.Split(s, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") {
			continue
		}
		if strings.Contains(code, "$PROFILE") && !strings.HasPrefix(code, "Write-Host '") {
			t.Errorf("line uses $PROFILE outside a printed message: %q", line)
		}
	}
}

func TestPowerShellSnippetIsCompatibleWithWindowsPowerShell(t *testing.T) {
	s, err := completioninstall.PowerShellSnippet("atk")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"??", "&&", "$IsWindows", "ForEach-Object -Parallel"} {
		if strings.Contains(s, banned) {
			t.Errorf("snippet holds %q, which Windows PowerShell 5.1 lacks", banned)
		}
	}
}

func TestPowerShellSnippetRejectsAnUnsafeName(t *testing.T) {
	for _, name := range []string{"", "Atk", "a'b", "a;b", "a b", "$(id)", "`id`", "a/b", "-a", "a-", "a.b", "a\nb"} {
		t.Run(name, func(t *testing.T) {
			s, err := completioninstall.PowerShellSnippet(name)
			if err == nil {
				t.Fatalf("PowerShellSnippet(%q) = %q, want an error", name, s)
			}
			if !errors.Is(err, validate.ErrInvalid) && !errors.Is(err, validate.ErrEmpty) {
				t.Errorf("error = %v, want one wrapping validate.ErrInvalid or validate.ErrEmpty", err)
			}
			if s != "" {
				t.Errorf("PowerShellSnippet(%q) returned a snippet with its error", name)
			}
		})
	}
}

func TestPowerShellSnippetParses(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not on PATH")
	}
	s, err := completioninstall.PowerShellSnippet("my-tool")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "snippet.ps1")
	if err := os.WriteFile(file, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	// -File passes the path as a real argument; with -Command every following argument is
	// joined into the command text and $args[0] is empty.
	const parse = "$errs = $null\n" +
		"[void][System.Management.Automation.Language.Parser]::ParseFile($args[0], [ref]$null, [ref]$errs)\n" +
		"foreach ($e in $errs) { [Console]::Out.WriteLine($e.Extent.StartLineNumber.ToString() + ': ' + $e.Message) }\n" +
		"if ($errs.Count) { exit 1 }\n"
	checker := filepath.Join(dir, "check.ps1")
	if err := os.WriteFile(checker, []byte(parse), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", checker, file).CombinedOutput()
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, out)
	}
}
