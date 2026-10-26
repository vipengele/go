package completioninstall

import (
	"fmt"
	"path"
	"strings"
)

// Shell is a shell a completion script is installed for. Its value is the argument the tool's
// `completion` command takes for that shell.
type Shell string

const (
	// Zsh is the Z shell.
	Zsh Shell = "zsh"
	// Bash is GNU Bash.
	Bash Shell = "bash"
	// Fish is the friendly interactive shell.
	Fish Shell = "fish"
	// PowerShell is PowerShell, Windows PowerShell and pwsh alike.
	PowerShell Shell = "powershell"
)

// Reason says why a completion script is not installed. Its value is a short lowercase phrase fit
// to show a user.
type Reason string

const (
	// ReasonDisabled is an installation the user or the consumer turned off.
	ReasonDisabled Reason = "disabled"
	// ReasonShellNotDetected is an installation with no shell named and none found in the
	// environment.
	ReasonShellNotDetected Reason = "shell not detected"
	// ReasonUnsupportedShell is a shell that is not one of the [Shell] constants.
	ReasonUnsupportedShell Reason = "unsupported shell"
	// ReasonNoHome is a target that needs the home directory when there is none.
	ReasonNoHome Reason = "no home directory"
	// ReasonUnsupportedOS is a shell this package installs nothing for on the operating system:
	// bash, zsh and fish on windows.
	ReasonUnsupportedOS Reason = "unsupported on this OS"
)

// Env holds the facts [Target] chooses a path from, already resolved by the caller. Every path in
// it is in forward-slash form, and an empty string is an absent value.
type Env struct {
	// GOOS is the operating system, as a value of runtime.GOOS.
	GOOS string
	// Home is the user's home directory.
	Home string
	// XDGDataHome is the value of XDG_DATA_HOME.
	XDGDataHome string
	// XDGConfigHome is the value of XDG_CONFIG_HOME.
	XDGConfigHome string
	// BrewPrefix is the Homebrew prefix, from HOMEBREW_PREFIX or `brew --prefix`.
	BrewPrefix string
	// BrewDirUsable reports that [ZshBrewDir] of BrewPrefix exists and is writable. Target
	// ignores it when BrewPrefix is empty.
	BrewDirUsable bool
}

// Location is where a completion script is written.
type Location struct {
	// Path is the file the script is written to, in forward-slash form.
	Path string
	// Note is what the user has to do for the shell to load the file, and "" when the shell
	// loads it as written.
	Note string
}

// ZshBrewDir returns the zsh function directory under the Homebrew prefix, the directory whose
// existence and writability [Env.BrewDirUsable] reports.
func ZshBrewDir(brewPrefix string) string {
	return path.Join(brewPrefix, "share", "zsh", "site-functions")
}

// Target returns where the completion script of the tool name for shell is written, given env. ok
// is false when nothing is written, and reason then says why; reason is "" when ok is true.
//
//   - zsh: <ZshBrewDir>/_<name> when env.BrewPrefix is set and env.BrewDirUsable, else
//     <Home>/.zsh/completions/_<name> with a Note on adding that directory to fpath.
//   - bash: <XDGDataHome, or Home/.local/share>/bash-completion/completions/<name>.
//   - fish: <XDGConfigHome, or Home/.config>/fish/completions/<name>.fish.
//   - powershell: <XDGConfigHome, or Home/.config>/powershell/Completions/<name>.ps1, and
//     <Home>/Documents/PowerShell/Completions/<name>.ps1 on windows, with a Note on dot-sourcing
//     the file from $PROFILE.
//
// bash, zsh and fish on windows are [ReasonUnsupportedOS], a shell that is not a [Shell] constant
// is [ReasonUnsupportedShell], and a target that needs Home when it is empty is [ReasonNoHome].
//
// Paths are joined with path.Join, in forward-slash form, so the table answers the same on every
// host; the caller converts the Path with filepath.FromSlash before writing it. Target touches no
// file system and reads no environment.
func Target(shell Shell, name string, env Env) (loc Location, reason Reason, ok bool) {
	windows := env.GOOS == "windows"
	switch shell {
	case Zsh:
		if windows {
			return Location{}, ReasonUnsupportedOS, false
		}
		if env.BrewPrefix != "" && env.BrewDirUsable {
			return Location{Path: path.Join(ZshBrewDir(env.BrewPrefix), "_"+name)}, "", true
		}
		if env.Home == "" {
			return Location{}, ReasonNoHome, false
		}
		dir := path.Join(env.Home, ".zsh", "completions")
		return Location{
			Path: path.Join(dir, "_"+name),
			Note: fmt.Sprintf("add %s to fpath before compinit runs, as in fpath=(%s $fpath) in ~/.zshrc", dir, dir),
		}, "", true
	case Bash:
		if windows {
			return Location{}, ReasonUnsupportedOS, false
		}
		base, ok := xdgOrHome(env.XDGDataHome, env.Home, ".local", "share")
		if !ok {
			return Location{}, ReasonNoHome, false
		}
		return Location{Path: path.Join(base, "bash-completion", "completions", name)}, "", true
	case Fish:
		if windows {
			return Location{}, ReasonUnsupportedOS, false
		}
		base, ok := xdgOrHome(env.XDGConfigHome, env.Home, ".config")
		if !ok {
			return Location{}, ReasonNoHome, false
		}
		return Location{Path: path.Join(base, "fish", "completions", name+".fish")}, "", true
	case PowerShell:
		var dir string
		if windows {
			if env.Home == "" {
				return Location{}, ReasonNoHome, false
			}
			dir = path.Join(env.Home, "Documents", "PowerShell", "Completions")
		} else {
			base, ok := xdgOrHome(env.XDGConfigHome, env.Home, ".config")
			if !ok {
				return Location{}, ReasonNoHome, false
			}
			dir = path.Join(base, "powershell", "Completions")
		}
		file := path.Join(dir, name+".ps1")
		return Location{
			Path: file,
			Note: fmt.Sprintf("dot-source %s from $PROFILE, as in . '%s'", file, file),
		}, "", true
	default:
		return Location{}, ReasonUnsupportedShell, false
	}
}

// xdgOrHome returns xdg when it is set, else home joined with the default's elements. ok is false
// when xdg is empty and so is home.
func xdgOrHome(xdg, home string, def ...string) (string, bool) {
	if xdg != "" {
		return xdg, true
	}
	if home == "" {
		return "", false
	}
	return path.Join(append([]string{home}, def...)...), true
}

// ParseShell returns the [Shell] a shell program names: a bare name such as "zsh", or a path to
// the program such as "/bin/zsh", the form $SHELL holds. The last element of a path separated by
// "/" or "\" is taken, and a ".exe" suffix and a login shell's leading "-" are dropped; "pwsh" and
// "powershell" are both [PowerShell]. ok is false for any other program and for "".
func ParseShell(program string) (shell Shell, ok bool) {
	base := program[strings.LastIndexAny(program, `/\`)+1:]
	base = strings.TrimPrefix(base, "-")
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	switch base {
	case "zsh":
		return Zsh, true
	case "bash":
		return Bash, true
	case "fish":
		return Fish, true
	case "pwsh", "powershell":
		return PowerShell, true
	default:
		return "", false
	}
}
