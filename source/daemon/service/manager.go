package service

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"go.vipengele.org/cli/spec/paths"
)

// Manager controls one service through the service manager of its operating
// system. Every method takes only a context that bounds the call.
type Manager interface {
	// Prepare creates what Install needs, such as the directories and files
	// the driver writes, without registering the service.
	Prepare(ctx context.Context) error
	// Install registers the service with the service manager.
	Install(ctx context.Context) error
	// Uninstall removes the service from the service manager.
	Uninstall(ctx context.Context) error
	// Start asks the service manager to start the service.
	Start(ctx context.Context) error
	// Stop asks the service manager to stop the service.
	Stop(ctx context.Context) error
	// Status reports the service as the service manager sees it.
	Status(ctx context.Context) (Status, error)
}

// New validates cfg against the rules of the target GOOS and returns the
// Manager of the driver for that GOOS. The target is runtime.GOOS unless
// WithGOOS sets another. The Manager is nil on every error.
//
// The checks run in a fixed order, and the first that fails decides the
// error:
//
//  1. An identity from WithIdentity that fails its Validate returns an error
//     wrapping ErrInvalidConfig.
//  2. A process inside a container returns a *ContainerError, which matches
//     ErrContainer, whatever cfg and the GOOS are. WithContainer decides
//     when set; otherwise paths.InContainerFor decides with the identity,
//     and paths.InContainer without one.
//  3. An invalid cfg, then an invalid Hardening from WithSystemdHardening,
//     then an invalid watchdog from WithSystemdNotify, then an invalid log
//     path from WithLaunchdLogs, returns an error wrapping ErrInvalidConfig.
//  4. A GOOS without a driver returns an error wrapping ErrUnsupported.
func New(cfg Config, opts ...Option) (Manager, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	goos := o.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if o.identity != nil {
		if err := o.identity.Validate(); err != nil {
			return nil, fmt.Errorf("%w: identity: %w", ErrInvalidConfig, err)
		}
	}
	if o.inContainer(goos) {
		return nil, &ContainerError{Instead: containerInstead}
	}
	if err := cfg.validate(goos); err != nil {
		return nil, err
	}
	if o.hardening != nil {
		if err := o.hardening.validate(); err != nil {
			return nil, err
		}
	}
	if err := o.notify.validate(); err != nil {
		return nil, err
	}
	if err := o.launchd.validate(); err != nil {
		return nil, err
	}
	switch goos {
	case "linux":
		h := DefaultHardening(cfg.Scope)
		if o.hardening != nil {
			h = *o.hardening
		}
		return &systemd{
			cfg:       cfg,
			hardening: h,
			notify:    o.notify,
			run:       execRunner,
			euid:      os.Geteuid(),
			lookupEnv: os.LookupEnv,
			lingerDir: "/var/lib/systemd/linger",
		}, nil
	case "darwin":
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	case "windows":
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	}
}

// inContainer reports whether the process runs inside a container on goos:
// the WithContainer seam when set, and otherwise the detection of
// cli/spec/paths, with the <PREFIX>_CONTAINER override when an identity is
// set.
func (o *options) inContainer(goos string) bool {
	if o.container != nil {
		return o.container()
	}
	p := paths.Probes{GOOS: goos, LookupEnv: o.lookupEnv}
	if o.identity != nil {
		return paths.InContainerFor(*o.identity, p)
	}
	return paths.InContainer(p)
}
