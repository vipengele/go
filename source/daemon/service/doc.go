// Package service fronts the operating system's service manager (systemd,
// launchd, the Windows Service Control Manager) through one driver per
// operating system behind the Manager interface.
//
// A service is described by a Config, which New validates before it hands
// back a Manager; a Config that fails validation is reported as
// ErrInvalidConfig. New reports ErrUnsupported for a GOOS with no driver.
//
// The state of an installed service is reported as a Status, whose State is
// one of a fixed set of values that marshal to lower-case text.
package service
