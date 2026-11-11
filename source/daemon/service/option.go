package service

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.vipengele.org/cli/spec"
)

// options holds the settings New applies before it validates the Config.
type options struct {
	goos      string
	identity  *spec.Identity
	container func() bool
	lookupEnv func(key string) (string, bool)
	// hardening is the WithSystemdHardening override; nil means the driver
	// applies DefaultHardening for the Config's Scope.
	hardening *Hardening
	// notify is the WithSystemdNotify setting; its zero value renders
	// Type=simple.
	notify notify
	// launchd is the WithLaunchdLogs setting; an empty path keeps the
	// driver's default for that stream.
	launchd launchdLogs
}

// launchdLogs holds the log file paths of a launchd job. An empty field keeps
// the driver's default for that stream.
type launchdLogs struct {
	stdout, stderr string
}

// validate reports the first path that is not a POSIX absolute path free of
// control characters and invalid UTF-8, wrapping ErrInvalidConfig. The rules
// do not depend on the target GOOS, because launchd runs only on macOS.
func (l launchdLogs) validate() error {
	for _, p := range []struct{ name, path string }{
		{"stdout", l.stdout},
		{"stderr", l.stderr},
	} {
		switch {
		case p.path == "":
		case !strings.HasPrefix(p.path, "/"):
			return fmt.Errorf("%w: WithLaunchdLogs: %s %q is not an absolute path", ErrInvalidConfig, p.name, p.path)
		case !utf8.ValidString(p.path):
			return fmt.Errorf("%w: WithLaunchdLogs: %s %q is not valid UTF-8", ErrInvalidConfig, p.name, p.path)
		case strings.IndexFunc(p.path, isControl) >= 0:
			return fmt.Errorf("%w: WithLaunchdLogs: %s %q contains a control character",
				ErrInvalidConfig, p.name, p.path)
		}
	}
	return nil
}

// Option configures New.
type Option func(*options)

// WithGOOS sets the GOOS whose rules validate the Config and whose driver New
// selects. It defaults to runtime.GOOS, and "" means runtime.GOOS. It exists
// to validate and render the service of another OS, for tests.
func WithGOOS(goos string) Option {
	return func(o *options) { o.goos = goos }
}

// WithIdentity names the tool whose service New manages, so that the user of
// the tool can override container detection with the <PREFIX>_CONTAINER
// environment variable, as paths.InContainerFor reads it. An identity that
// fails its Validate makes New return an error wrapping ErrInvalidConfig.
func WithIdentity(id spec.Identity) Option {
	return func(o *options) { o.identity = &id }
}

// WithContainer replaces container detection: New refuses with a
// *ContainerError exactly when inContainer returns true. It wins over the
// environment and WithIdentity, and the host is not probed. A nil
// inContainer leaves detection in place.
func WithContainer(inContainer func() bool) Option {
	return func(o *options) { o.container = inContainer }
}

// WithSystemdHardening replaces DefaultHardening with h for a systemd unit.
// New validates h on every GOOS and returns an error wrapping
// ErrInvalidConfig for an invalid one; drivers other than systemd ignore it.
func WithSystemdHardening(h Hardening) Option {
	return func(o *options) {
		h.ReadWritePaths = append([]string(nil), h.ReadWritePaths...)
		o.hardening = &h
	}
}

// WithSystemdNotify makes a systemd unit Type=notify instead of Type=simple,
// and, when watchdog is above zero, sets WatchdogSec= to watchdog. A
// watchdog of 0 writes no WatchdogSec= line.
//
// The binary must then send READY=1, as daemon.WithSystemd does, or systemd
// fails the unit's start at its start timeout. With a watchdog it must also
// send WATCHDOG=1 within every interval, or systemd restarts it;
// daemon.WithSystemd sends it every half of WATCHDOG_USEC, which systemd sets
// from WatchdogSec=.
//
// New validates watchdog on every GOOS and returns an error wrapping
// ErrInvalidConfig when it is negative or not a whole number of
// milliseconds; drivers other than systemd ignore the option.
func WithSystemdNotify(watchdog time.Duration) Option {
	return func(o *options) { o.notify = notify{enabled: true, watchdog: watchdog} }
}

// WithLaunchdLogs sets the files a launchd job writes its standard output and
// standard error to, as the StandardOutPath and StandardErrorPath keys of its
// property list. An empty string keeps the driver's default for that stream.
//
// New validates each non-empty path on every GOOS and returns an error
// wrapping ErrInvalidConfig when it is not a POSIX absolute path or contains a
// control character or invalid UTF-8; the systemd driver ignores the option.
func WithLaunchdLogs(stdout, stderr string) Option {
	return func(o *options) { o.launchd = launchdLogs{stdout: stdout, stderr: stderr} }
}
