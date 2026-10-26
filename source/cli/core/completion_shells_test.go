package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cli "go.vipengele.org/cli/core"
)

// shellLoad is how one shell loads a generated completion script and reports what it registered.
type shellLoad struct {
	// shell is the argument to "completion".
	shell string
	// program is the executable that loads the script, looked up on PATH.
	program string
	// required fails the test, rather than skipping the load, when program is not on PATH.
	required bool
	// marker is a string every generated script for shell contains.
	marker string
	// args run program so that it loads the script named by $COMPLETION_SCRIPT and then prints
	// what the script registered for the command "tool".
	args []string
	// want is printed by args only when the script registered completion for "tool".
	want string
}

// shellLoads are the shells "completion" generates a script for. Each loads the script with no
// startup file of its own: the environment is scrubbed and its home and config directories are
// empty, so neither the runner's rc files nor its installed completions take part.
func shellLoads() []shellLoad {
	return []shellLoad{
		{
			shell:    "bash",
			program:  "bash",
			required: true,
			marker:   "complete -o default -F __start_tool tool",
			args: []string{"--noprofile", "--norc", "-c",
				`source "$COMPLETION_SCRIPT" && complete -p tool`},
			want: "-F __start_tool tool",
		},
		{
			shell:   "zsh",
			program: "zsh",
			marker:  "#compdef tool",
			// compinit defines compdef, which the script calls on load; -D writes no dump file
			// and -u skips the ownership check of the fpath directories.
			args: []string{"-f", "-c",
				`autoload -U compinit && compinit -u -D && source "$COMPLETION_SCRIPT" && ` +
					`print -r -- "comp=$_comps[tool]"`},
			want: "comp=_tool",
		},
		{
			shell:   "fish",
			program: "fish",
			marker:  "complete -c tool",
			// complete -c with no other option prints the completions registered for the command.
			args: []string{"-c", `source $COMPLETION_SCRIPT; and complete -c tool`},
			want: "__tool_prepare_completions",
		},
		{
			shell:   "powershell",
			program: "pwsh",
			marker:  "Register-ArgumentCompleter -CommandName 'tool'",
			// Stop turns any error the script raises into a non-zero exit.
			args: []string{"-NoProfile", "-NonInteractive", "-Command",
				`$ErrorActionPreference = 'Stop'; . $env:COMPLETION_SCRIPT; ` +
					`if (${__toolCompleterBlock} -is [scriptblock]) { 'block=registered' }`},
			want: "block=registered",
		},
	}
}

// shellEnv is the whole environment a shell loads a script in: PATH to find the shell's own
// helpers, and every home, config, data and cache directory pointing into home.
func shellEnv(home, script string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"ZDOTDIR=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_DATA_DIRS=" + filepath.Join(home, ".local", "share"),
		"TERM=dumb",
		"LANG=C",
		"POWERSHELL_TELEMETRY_OPTOUT=1",
		"POWERSHELL_UPDATECHECK=Off",
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"COMPLETION_SCRIPT=" + script,
	}
}

// Every generated script is loaded by its own shell without error and registers completion for
// the tool's command. Generation always runs; the load runs where the shell is installed, and bash
// is required.
func TestCompletionScriptLoadsInItsShell(t *testing.T) {
	for _, sl := range shellLoads() {
		t.Run(sl.shell, func(t *testing.T) {
			got := runApp(t, newApp(cli.WithCompletion()), "completion", sl.shell)
			if got.code != cli.ExitOK || got.stderr != "" {
				t.Fatalf("completion %s = %d, stderr %q; want 0 and no stderr", sl.shell, got.code, got.stderr)
			}
			if strings.TrimSpace(got.stdout) == "" {
				t.Fatalf("completion %s wrote an empty script", sl.shell)
			}
			if !strings.Contains(got.stdout, sl.marker) {
				t.Fatalf("completion %s wrote a script without %q:\n%s", sl.shell, sl.marker, got.stdout)
			}

			program, err := exec.LookPath(sl.program)
			if err != nil {
				if sl.required {
					t.Fatalf("%s is not on PATH, and loading the %s script requires it: %v",
						sl.program, sl.shell, err)
				}
				t.Skipf("%s is not on PATH; the %s script was generated but not loaded", sl.program, sl.shell)
			}

			home := t.TempDir()
			script := filepath.Join(t.TempDir(), "tool."+sl.shell)
			if err := os.WriteFile(script, []byte(got.stdout), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, program, sl.args...)
			cmd.Env = shellEnv(home, script)
			cmd.Dir = home
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s loading the %s script: %v\n%s", sl.program, sl.shell, err, out)
			}
			if !strings.Contains(string(out), sl.want) {
				t.Errorf("%s loading the %s script printed %q, want it to contain %q",
					sl.program, sl.shell, out, sl.want)
			}
		})
	}
}
