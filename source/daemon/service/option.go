package service

import "go.vipengele.org/cli/spec"

// options holds the settings New applies before it validates the Config.
type options struct {
	goos      string
	identity  *spec.Identity
	container func() bool
	lookupEnv func(key string) (string, bool)
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
