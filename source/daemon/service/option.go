package service

import (
	"time"

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
