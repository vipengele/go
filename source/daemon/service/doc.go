// Package service fronts the operating system's service manager (systemd,
// launchd, the Windows Service Control Manager) through one driver per
// operating system behind the Manager interface.
//
// A service is described by a Config, which New validates before it hands
// back a Manager; a Config that fails validation is reported as
// ErrInvalidConfig. New reports ErrUnsupported for a GOOS with no driver.
//
// # Containers
//
// Inside a container the container runtime supervises the process, and no
// service manager is there to install into. New refuses there before it looks
// at the Config or the GOOS: it returns a *ContainerError, which matches
// ErrContainer under errors.Is and whose Instead field says what to do in
// place of a service, which is to run the program as the image's entrypoint
// under a restart policy.
//
// New detects a container with go.vipengele.org/cli/spec/paths. WithIdentity
// names the tool, so that its user can override the detection with the
// <PREFIX>_CONTAINER environment variable, as paths.InContainerFor reads it;
// without it New uses the paths.InContainer heuristic alone. An identity that
// fails its Validate is reported as ErrInvalidConfig. WithContainer replaces
// detection altogether, and wins over the environment and WithIdentity; tests
// use it to keep New from probing the host.
//
// The state of an installed service is reported as a Status, whose State is
// one of a fixed set of values that marshal to lower-case text.
package service
