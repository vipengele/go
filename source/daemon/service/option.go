package service

// options holds the settings New applies before it validates the Config.
type options struct {
	goos string
	// hardening is the WithSystemdHardening override; nil means the driver
	// applies DefaultHardening for the Config's Scope.
	hardening *Hardening
}

// Option configures New.
type Option func(*options)

// WithGOOS sets the GOOS whose rules validate the Config and whose driver New
// selects. It defaults to runtime.GOOS, and "" means runtime.GOOS. It exists
// to validate and render the service of another OS, for tests.
func WithGOOS(goos string) Option {
	return func(o *options) { o.goos = goos }
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
