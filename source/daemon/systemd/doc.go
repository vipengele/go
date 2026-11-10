// Package systemd speaks the parts of the systemd service protocol a daemon needs: readiness and
// watchdog notification, and socket activation. It uses the standard library only.
//
// # Environment
//
// systemd hands a service its settings in environment variables. Every function here takes a
// lookup function of the shape func(key string) (string, bool), the shape of spec.Env.LookupEnv
// and os.LookupEnv, and reads the variables through it alone. Nothing in the package calls
// os.Getenv or changes the process environment, so the variables stay visible to child processes
// and a test supplies its own. The lookup must not be nil.
//
// # Off Linux and outside systemd
//
// Every function is a no-op when runtime.GOOS is not linux, and when the variables it reads are
// absent or empty, which is how a process started outside systemd sees them. A cross-platform
// daemon keeps one code path and calls them unconditionally.
//
// # Notification
//
// Notify sends one state, such as Ready, Stopping or Watchdog, as a single datagram to the
// unixgram socket named by NOTIFY_SOCKET: an absolute path, or an abstract socket name written
// with a leading @. WatchdogInterval reads WATCHDOG_USEC and WATCHDOG_PID, which systemd sets for
// a unit with WatchdogSec=; the service sends Watchdog more often than the interval it returns,
// conventionally at half of it.
//
// # Socket activation
//
// Listeners adopts the listening sockets systemd opened for the process, announced by LISTEN_PID,
// LISTEN_FDS and LISTEN_FDNAMES, and returns them keyed by name. It wraps each inherited
// descriptor in a net.Listener and closes the descriptor itself, so it is called once per
// process: a second call would wrap whatever has since reused those descriptor numbers.
//
// The design is recorded in the repository's
// docs/adr/0030-systemd-readiness-and-socket-activation-use-the-standard-library-and-an-injected-environment.md.
package systemd
