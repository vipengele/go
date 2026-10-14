// Package paths resolves the directories a command-line tool keeps its files in: one directory
// for each [Kind], configuration, state, cache, data and logs.
//
// [New] resolves them from the tool's [spec.Identity] and an [Options] value, and returns them as
// a [Dirs]. Resolving reads only the environment and the user's home directory, through the
// seams Options carries; it creates, stats and opens nothing on the file system.
//
// # Precedence
//
// Each kind takes the first of these that applies:
//
//  1. its explicit option, Options.ConfigDir, StateDir, CacheDir, DataDir or LogDir, used as
//     given;
//  2. Options.Home, a home root under which the kinds are the subdirectories config, state,
//     cache, data and log;
//  3. the environment variable <PREFIX>_HOME, as [spec.Identity.EnvVar] names it with the suffix
//     "HOME", a home root like Options.Home;
//  4. on the XDG layout only, the kind's XDG base directory, XDG_CONFIG_HOME, XDG_STATE_HOME,
//     XDG_CACHE_HOME or XDG_DATA_HOME, with Identity.DirName appended;
//  5. the platform default below.
//
// A home root is used as given, without Identity.DirName appended. An environment variable set to
// the empty string counts as unset, and so does an XDG base directory that is not an absolute
// path, as the XDG Base Directory Specification requires.
//
// # Layouts
//
// Options.GOOS, or runtime.GOOS when it is empty, selects the layout. It selects which
// directories are used, not the path separator, which is always the running system's. In the
// table <app> is Identity.DirName and ~ is the user's home directory.
//
// The XDG layout serves "linux" and every GOOS other than "darwin" and "windows":
//
//	config  $XDG_CONFIG_HOME/<app>, or ~/.config/<app>
//	state   $XDG_STATE_HOME/<app>, or ~/.local/state/<app>
//	cache   $XDG_CACHE_HOME/<app>, or ~/.cache/<app>
//	data    $XDG_DATA_HOME/<app>, or ~/.local/share/<app>
//	log     <state>/log
//
// The "darwin" layout ignores the XDG variables:
//
//	config  ~/Library/Application Support/<app>
//	state   <config>/state
//	data    <config>/data
//	cache   ~/Library/Caches/<app>
//	log     ~/Library/Logs/<app>
//
// The "windows" layout reads %AppData% and %LocalAppData%, falling back to ~/AppData/Roaming and
// ~/AppData/Local when either is unset:
//
//	config  %AppData%/<app>
//	state   %LocalAppData%/<app>/state
//	data    %LocalAppData%/<app>/data
//	cache   %LocalAppData%/<app>/cache
//	log     %LocalAppData%/<app>/log
//
// A kind derived from another, as log is from state on the XDG layout, follows that kind as it is
// resolved: an explicit Options.StateDir moves the default log directory with it. An explicit
// option for the derived kind itself takes precedence over the derivation.
//
// No two kinds share a directory in any default layout. New does not check explicit options for
// collisions: two explicit options naming one directory resolve to that directory twice.
//
// The home directory is looked up only when some kind needs it, so a tool whose every kind is
// set by an option or the environment resolves without one. When a kind needs it and it cannot be
// found, New returns an error.
package paths
