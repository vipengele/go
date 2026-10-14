// Package paths resolves the directories a command-line tool keeps its files in: one directory
// for each [Kind], configuration, state, cache, data and logs.
//
// [Resolve] resolves the kinds a caller names from the tool's [spec.Identity] and an [Options]
// value, and returns them as a [Dirs]; [New] resolves every kind. Resolving reads only the
// environment and the user's home directory, through the seams Options carries; it creates,
// stats and opens nothing on the file system. A kind Resolve is not asked for is left
// unresolved: its accessor returns "" and [Dirs.Ensure] reports it.
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
// Resolve follows a derivation only for a kind it is asked for: resolving log alone on the XDG
// layout reads state to derive it, and leaves state itself unresolved.
//
// No two kinds share a directory in any default layout. Resolving does not check explicit options
// for collisions: two explicit options naming one directory resolve to that directory twice.
//
// The home directory is looked up at most once, and only when a requested kind, or the kind it
// derives from, needs it. A tool whose requested kinds are set by an option or the environment
// therefore resolves without one: state alone resolves from an absolute XDG_STATE_HOME even when
// config, cache and data would fall back to the home directory. When a requested kind needs the
// home directory and it cannot be found, Resolve and New return an error.
//
// # Containers
//
// [InContainer] reports whether the process appears to run inside a container, so a caller can
// choose a different location or policy there. It is a heuristic and changes no path. It looks
// for /.dockerenv, /run/.containerenv, the container and KUBERNETES_SERVICE_HOST variables and
// a mention of docker, kubepods or containerd in /proc/1/cgroup. Under cgroup v2 a container
// sees "0::/", so the cgroup signal is not reliable there. It is always false off Linux.
package paths
