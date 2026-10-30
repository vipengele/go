package completioninstall

import (
	"fmt"
	"strings"

	"go.vipengele.org/commons/validate"
)

// SnippetFunc is the name of the shell function [Snippet] defines.
const SnippetFunc = "completion_target"

// snippetName stands for the tool name in snippetTemplate.
const snippetName = "@NAME@"

// snippetTemplate is the POSIX-sh fragment [Snippet] renders. Every helper variable and
// function it defines starts with _ci_, so it leaves the embedding script's own names alone.
//
// _ci_clean is path.Clean: it drops empty and "." elements, resolves ".." against the element
// before it, keeps a leading ".." of a relative path, drops one at the root, and answers "." for
// an empty result. Each path is cleaned whole, as path.Join cleans its result, so a trailing or
// doubled slash in HOME, XDG_DATA_HOME, XDG_CONFIG_HOME or HOMEBREW_PREFIX selects the path
// Target selects.
const snippetTemplate = `# completion_target SHELL selects where the completion script of ` + snippetName + `
# is installed for SHELL: zsh, bash, fish or powershell.
#
# On success it sets COMPLETION_PATH to the file, COMPLETION_NOTE to what the user has to do for
# the shell to load it ("" when nothing), prints COMPLETION_PATH and returns 0. With no home
# directory where the target needs one, it prints nothing and returns 1; for any other SHELL it
# prints nothing and returns 2. Both variables are "" on failure. It writes no file.
_ci_clean() {
	_ci_in=$1
	_ci_root=
	case $_ci_in in /*) _ci_root=/ ;; esac
	_ci_out=
	_ci_pop=0
	_ci_ifs=$IFS
	case $- in *f*) _ci_noglob=1 ;; *) _ci_noglob= ;; esac
	set -f
	IFS=/
	for _ci_el in $_ci_in; do
		case $_ci_el in
		'' | .) ;;
		..)
			if [ "$_ci_pop" -gt 0 ]; then
				case $_ci_out in
				*/*) _ci_out=${_ci_out%/*} ;;
				*) _ci_out= ;;
				esac
				_ci_pop=$((_ci_pop - 1))
			elif [ -z "$_ci_root" ]; then
				_ci_out=${_ci_out:+$_ci_out/}..
			fi
			;;
		*)
			_ci_out=${_ci_out:+$_ci_out/}$_ci_el
			_ci_pop=$((_ci_pop + 1))
			;;
		esac
	done
	IFS=$_ci_ifs
	[ -n "$_ci_noglob" ] || set +f
	_ci_out=$_ci_root$_ci_out
	_ci_clean_out=${_ci_out:-.}
}

completion_target() {
	COMPLETION_PATH=
	COMPLETION_NOTE=
	_ci_name='` + snippetName + `'
	_ci_home=${HOME:-}
	case ${1:-} in
	zsh)
		_ci_prefix=${HOMEBREW_PREFIX:-}
		if [ -z "$_ci_prefix" ] && command -v brew >/dev/null 2>&1; then
			_ci_prefix=$(brew --prefix 2>/dev/null) || _ci_prefix=
		fi
		if [ -n "$_ci_prefix" ]; then
			_ci_clean "$_ci_prefix/share/zsh/site-functions"
			if [ -d "$_ci_clean_out" ] && [ -w "$_ci_clean_out" ]; then
				_ci_clean "$_ci_prefix/share/zsh/site-functions/_$_ci_name"
				COMPLETION_PATH=$_ci_clean_out
			fi
		fi
		if [ -z "$COMPLETION_PATH" ]; then
			[ -n "$_ci_home" ] || return 1
			_ci_clean "$_ci_home/.zsh/completions"
			_ci_dir=$_ci_clean_out
			_ci_clean "$_ci_home/.zsh/completions/_$_ci_name"
			COMPLETION_PATH=$_ci_clean_out
			COMPLETION_NOTE="add $_ci_dir to fpath before compinit runs, as in fpath=($_ci_dir \$fpath) in ~/.zshrc"
		fi
		;;
	bash)
		if [ -n "${XDG_DATA_HOME:-}" ]; then
			_ci_base=$XDG_DATA_HOME
		elif [ -n "$_ci_home" ]; then
			_ci_base=$_ci_home/.local/share
		else
			return 1
		fi
		_ci_clean "$_ci_base/bash-completion/completions/$_ci_name"
		COMPLETION_PATH=$_ci_clean_out
		;;
	fish | powershell)
		if [ -n "${XDG_CONFIG_HOME:-}" ]; then
			_ci_base=$XDG_CONFIG_HOME
		elif [ -n "$_ci_home" ]; then
			_ci_base=$_ci_home/.config
		else
			return 1
		fi
		if [ "$1" = fish ]; then
			_ci_clean "$_ci_base/fish/completions/$_ci_name.fish"
			COMPLETION_PATH=$_ci_clean_out
		else
			_ci_clean "$_ci_base/powershell/Completions/$_ci_name.ps1"
			COMPLETION_PATH=$_ci_clean_out
			COMPLETION_NOTE="dot-source $COMPLETION_PATH from \$PROFILE, as in . '$COMPLETION_PATH'"
		fi
		;;
	*)
		return 2
		;;
	esac
	printf '%s\n' "$COMPLETION_PATH"
}
`

// Snippet returns a POSIX-sh fragment for a tool's install script that selects where the
// completion script of the tool name is installed, choosing the path [Target] chooses on a Unix
// host. name must be a slug as [spec.Identity] accepts its Name: lowercase letters, digits and
// '-', which need no quoting in sh. Any other name is an error wrapping [validate.ErrEmpty] or
// [validate.ErrInvalid].
//
// The fragment defines the function [SnippetFunc], called as `completion_target SHELL` with
// SHELL one of zsh, bash, fish and powershell, the argument of the tool's `completion` command.
// On success it sets COMPLETION_PATH to the file and COMPLETION_NOTE to [Location.Note], prints
// the path and returns 0. Where Target answers [ReasonNoHome] it prints nothing and returns 1, and
// for any other SHELL, [ReasonUnsupportedShell], it prints nothing and returns 2; both variables
// are then "". It creates no directory and writes no file; the install script writes the output of
// `<tool> completion SHELL` to COMPLETION_PATH.
//
// It reads the environment as [Install] does on a Unix host: HOME is the home directory, and
// XDG_DATA_HOME, XDG_CONFIG_HOME and HOMEBREW_PREFIX count as unset when empty. For zsh, when
// HOMEBREW_PREFIX is empty and brew is on PATH, the prefix is the output of `brew --prefix`. The
// Homebrew zsh directory is usable when `[ -d ]` and `[ -w ]` both hold for it. Every path is
// cleaned as path.Join cleans it, so a trailing or doubled slash, a "." or a ".." in any of those
// variables yields the path Target yields. The fragment is safe under `set -u`; the function
// restores IFS and the noglob option it changes.
func Snippet(name string) (string, error) {
	if err := validate.Slug("name", name); err != nil {
		return "", fmt.Errorf("completioninstall: invalid tool name: %w", err)
	}
	return strings.ReplaceAll(snippetTemplate, snippetName, name), nil
}
