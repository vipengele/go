package completioninstall_test

import (
	"testing"

	"go.vipengele.org/cli/spec/completioninstall"
)

const (
	tool = "atk"
	home = "/home/u"
)

func TestTarget(t *testing.T) {
	const (
		zshHomeNote = "add /home/u/.zsh/completions to fpath before compinit runs, as in " +
			"fpath=(/home/u/.zsh/completions $fpath) in ~/.zshrc"
		psUnix    = "/home/u/.config/powershell/Completions/atk.ps1"
		psXDG     = "/xdg/config/powershell/Completions/atk.ps1"
		psWindows = "C:/Users/u/Documents/PowerShell/Completions/atk.ps1"
	)
	psNote := func(file string) string {
		return "dot-source " + file + " from $PROFILE, as in . '" + file + "'"
	}
	tests := []struct {
		name   string
		shell  completioninstall.Shell
		env    completioninstall.Env
		want   completioninstall.Location
		reason completioninstall.Reason
	}{
		{
			name:  "zsh writes to the Homebrew site-functions directory when it is usable",
			shell: completioninstall.Zsh,
			env:   completioninstall.Env{GOOS: "darwin", Home: home, BrewPrefix: "/opt/homebrew", BrewDirUsable: true},
			want:  completioninstall.Location{Path: "/opt/homebrew/share/zsh/site-functions/_atk"},
		},
		{
			name:  "zsh with a usable Homebrew directory needs no home",
			shell: completioninstall.Zsh,
			env:   completioninstall.Env{GOOS: "darwin", BrewPrefix: "/opt/homebrew", BrewDirUsable: true},
			want:  completioninstall.Location{Path: "/opt/homebrew/share/zsh/site-functions/_atk"},
		},
		{
			name:  "zsh falls back to the home directory when the Homebrew directory is unusable",
			shell: completioninstall.Zsh,
			env:   completioninstall.Env{GOOS: "darwin", Home: home, BrewPrefix: "/opt/homebrew"},
			want:  completioninstall.Location{Path: "/home/u/.zsh/completions/_atk", Note: zshHomeNote},
		},
		{
			name:  "zsh without Homebrew writes under the home directory with an fpath note",
			shell: completioninstall.Zsh,
			env:   completioninstall.Env{GOOS: "linux", Home: home, BrewDirUsable: true},
			want:  completioninstall.Location{Path: "/home/u/.zsh/completions/_atk", Note: zshHomeNote},
		},
		{
			name:   "zsh without a usable Homebrew directory needs a home",
			shell:  completioninstall.Zsh,
			env:    completioninstall.Env{GOOS: "linux", BrewPrefix: "/opt/homebrew"},
			reason: completioninstall.ReasonNoHome,
		},
		{
			name:  "bash defaults under the home directory",
			shell: completioninstall.Bash,
			env:   completioninstall.Env{GOOS: "linux", Home: home},
			want:  completioninstall.Location{Path: "/home/u/.local/share/bash-completion/completions/atk"},
		},
		{
			name:  "bash honours XDG_DATA_HOME",
			shell: completioninstall.Bash,
			env:   completioninstall.Env{GOOS: "linux", Home: home, XDGDataHome: "/xdg/data", XDGConfigHome: "/xdg/config"},
			want:  completioninstall.Location{Path: "/xdg/data/bash-completion/completions/atk"},
		},
		{
			name:  "bash with XDG_DATA_HOME needs no home",
			shell: completioninstall.Bash,
			env:   completioninstall.Env{GOOS: "linux", XDGDataHome: "/xdg/data"},
			want:  completioninstall.Location{Path: "/xdg/data/bash-completion/completions/atk"},
		},
		{
			name:   "bash without XDG_DATA_HOME needs a home",
			shell:  completioninstall.Bash,
			env:    completioninstall.Env{GOOS: "linux", XDGConfigHome: "/xdg/config"},
			reason: completioninstall.ReasonNoHome,
		},
		{
			name:  "fish defaults under the home directory",
			shell: completioninstall.Fish,
			env:   completioninstall.Env{GOOS: "darwin", Home: home, XDGDataHome: "/xdg/data"},
			want:  completioninstall.Location{Path: "/home/u/.config/fish/completions/atk.fish"},
		},
		{
			name:  "fish honours XDG_CONFIG_HOME and needs no home with it",
			shell: completioninstall.Fish,
			env:   completioninstall.Env{GOOS: "linux", XDGConfigHome: "/xdg/config"},
			want:  completioninstall.Location{Path: "/xdg/config/fish/completions/atk.fish"},
		},
		{
			name:   "fish without XDG_CONFIG_HOME needs a home",
			shell:  completioninstall.Fish,
			env:    completioninstall.Env{GOOS: "linux"},
			reason: completioninstall.ReasonNoHome,
		},
		{
			name:  "powershell on Unix defaults under the home directory",
			shell: completioninstall.PowerShell,
			env:   completioninstall.Env{GOOS: "linux", Home: home},
			want:  completioninstall.Location{Path: psUnix, Note: psNote(psUnix)},
		},
		{
			name:  "powershell on Unix honours XDG_CONFIG_HOME and needs no home with it",
			shell: completioninstall.PowerShell,
			env:   completioninstall.Env{GOOS: "darwin", XDGConfigHome: "/xdg/config"},
			want:  completioninstall.Location{Path: psXDG, Note: psNote(psXDG)},
		},
		{
			name:   "powershell on Unix without XDG_CONFIG_HOME needs a home",
			shell:  completioninstall.PowerShell,
			env:    completioninstall.Env{GOOS: "linux"},
			reason: completioninstall.ReasonNoHome,
		},
		{
			name:  "powershell on Windows writes under Documents and ignores XDG_CONFIG_HOME",
			shell: completioninstall.PowerShell,
			env:   completioninstall.Env{GOOS: "windows", Home: "C:/Users/u", XDGConfigHome: "/xdg/config"},
			want:  completioninstall.Location{Path: psWindows, Note: psNote(psWindows)},
		},
		{
			name:   "powershell on Windows needs a home even with XDG_CONFIG_HOME",
			shell:  completioninstall.PowerShell,
			env:    completioninstall.Env{GOOS: "windows", XDGConfigHome: "/xdg/config"},
			reason: completioninstall.ReasonNoHome,
		},
		{
			name:   "zsh is unsupported on Windows",
			shell:  completioninstall.Zsh,
			env:    completioninstall.Env{GOOS: "windows", Home: home, BrewPrefix: "/opt/homebrew", BrewDirUsable: true},
			reason: completioninstall.ReasonUnsupportedOS,
		},
		{
			name:   "bash is unsupported on Windows",
			shell:  completioninstall.Bash,
			env:    completioninstall.Env{GOOS: "windows", Home: home},
			reason: completioninstall.ReasonUnsupportedOS,
		},
		{
			name:   "fish is unsupported on Windows",
			shell:  completioninstall.Fish,
			env:    completioninstall.Env{GOOS: "windows", Home: home},
			reason: completioninstall.ReasonUnsupportedOS,
		},
		{
			name:   "Windows is reported before a missing home",
			shell:  completioninstall.Bash,
			env:    completioninstall.Env{GOOS: "windows"},
			reason: completioninstall.ReasonUnsupportedOS,
		},
		{
			name:   "an unknown shell is unsupported",
			shell:  "tcsh",
			env:    completioninstall.Env{GOOS: "linux", Home: home},
			reason: completioninstall.ReasonUnsupportedShell,
		},
		{
			name:   "the empty shell is unsupported",
			shell:  "",
			env:    completioninstall.Env{GOOS: "linux", Home: home},
			reason: completioninstall.ReasonUnsupportedShell,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason, ok := completioninstall.Target(tt.shell, tool, tt.env)
			if wantOK := tt.reason == ""; ok != wantOK {
				t.Fatalf("Target() ok = %v, want %v (reason %q)", ok, wantOK, reason)
			}
			if reason != tt.reason {
				t.Errorf("Target() reason = %q, want %q", reason, tt.reason)
			}
			if got != tt.want {
				t.Errorf("Target() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestZshBrewDir(t *testing.T) {
	if got, want := completioninstall.ZshBrewDir("/usr/local/"), "/usr/local/share/zsh/site-functions"; got != want {
		t.Errorf("ZshBrewDir() = %q, want %q", got, want)
	}
}

func TestParseShell(t *testing.T) {
	tests := []struct {
		program string
		want    completioninstall.Shell
		ok      bool
	}{
		{program: "zsh", want: completioninstall.Zsh, ok: true},
		{program: "/bin/zsh", want: completioninstall.Zsh, ok: true},
		{program: "/opt/homebrew/bin/bash", want: completioninstall.Bash, ok: true},
		{program: "-bash", want: completioninstall.Bash, ok: true},
		{program: "/usr/bin/fish", want: completioninstall.Fish, ok: true},
		{program: "pwsh", want: completioninstall.PowerShell, ok: true},
		{program: "/usr/local/bin/pwsh", want: completioninstall.PowerShell, ok: true},
		{program: "powershell", want: completioninstall.PowerShell, ok: true},
		{program: `C:\Program Files\PowerShell\7\pwsh.exe`, want: completioninstall.PowerShell, ok: true},
		{program: `C:\Windows\System32\WindowsPowerShell\v1.0\PowerShell.exe`, want: completioninstall.PowerShell, ok: true},
		{program: ""},
		{program: "/bin/"},
		{program: "/bin/sh"},
		{program: "tcsh"},
		{program: "zshell"},
	}
	for _, tt := range tests {
		t.Run(tt.program, func(t *testing.T) {
			got, ok := completioninstall.ParseShell(tt.program)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ParseShell(%q) = %q, %v, want %q, %v", tt.program, got, ok, tt.want, tt.ok)
			}
		})
	}
}
