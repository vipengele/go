package service

import (
	"context"
	"fmt"
	"runtime"
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
// WithGOOS sets another. An invalid cfg returns an error wrapping
// ErrInvalidConfig; a GOOS without a driver returns an error wrapping
// ErrUnsupported. The Manager is nil on every error.
func New(cfg Config, opts ...Option) (Manager, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	goos := o.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if err := cfg.validate(goos); err != nil {
		return nil, err
	}
	if o.hardening != nil {
		if err := o.hardening.validate(); err != nil {
			return nil, err
		}
	}
	switch goos {
	case "linux":
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	case "darwin":
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	case "windows":
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, goos)
	}
}
