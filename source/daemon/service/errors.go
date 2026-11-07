package service

import "errors"

var (
	// ErrInvalidConfig is returned when a Config fails validation.
	ErrInvalidConfig = errors.New("service: invalid config")

	// ErrUnsupported is returned by New when the target GOOS has no service
	// manager driver.
	ErrUnsupported = errors.New("service: unsupported GOOS")

	// ErrNotInstalled is returned when an operation needs the service to be
	// installed and it is not.
	ErrNotInstalled = errors.New("service: not installed")

	// ErrPrivilegesRequired is returned when an operation needs privileges the
	// caller lacks.
	ErrPrivilegesRequired = errors.New("service: privileges required")

	// ErrManagerUnreachable is returned when the service manager could not be
	// reached, such as a user manager with no session bus.
	ErrManagerUnreachable = errors.New("service: service manager unreachable")
)
