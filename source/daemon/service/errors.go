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

	// ErrContainer is returned by New when the process runs inside a
	// container. The error New returns is a *ContainerError, which matches it
	// under errors.Is.
	ErrContainer = errors.New("service: running in a container")
)

// containerInstead is what a process in a container does in place of
// installing a service.
const containerInstead = "the container runtime supervises this process; " +
	"run it as the image's entrypoint and give the container a restart policy " +
	"that restarts on exit 0 (Docker --restart unless-stopped, " +
	"Compose restart: unless-stopped, or a Kubernetes Deployment)"

// ContainerError reports that New refuses to manage a service because the
// process runs inside a container, where the container runtime, not a
// service manager, supervises it. Instead says what to do in its place.
type ContainerError struct {
	Instead string
}

// Error names the refusal and what to do instead.
func (e *ContainerError) Error() string {
	return "service: running in a container: " + e.Instead
}

// Is reports whether target is ErrContainer.
func (e *ContainerError) Is(target error) bool {
	return target == ErrContainer
}
