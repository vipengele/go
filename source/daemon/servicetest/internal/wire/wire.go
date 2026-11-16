// Package wire is the request the servicetest harness sends to the in-container
// driver as JSON on its standard input. Both sides import it, so the two cannot
// disagree on the shape.
package wire

import (
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon/service"
)

// Op is the operation the driver performs on the service.
type Op string

const (
	// OpInstall installs the service.
	OpInstall Op = "install"
	// OpUninstall removes the service.
	OpUninstall Op = "uninstall"
	// OpStatus reports the service's status.
	OpStatus Op = "status"
)

// Request is one driver invocation: the operation, the service Config and the
// settings a consumer would otherwise pass to service.New as Options. A nil
// pointer means the option is not applied.
type Request struct {
	Op     Op
	Config service.Config
	// Notify is the WithSystemdNotify setting.
	Notify *Notify
	// Hardening is the WithSystemdHardening setting.
	Hardening *service.Hardening
	// Identity is the WithIdentity setting.
	Identity *spec.Identity
}

// Notify holds the input of service.WithSystemdNotify.
type Notify struct {
	// Watchdog is the WatchdogSec= interval, encoded in JSON as nanoseconds;
	// 0 writes no WatchdogSec= line.
	Watchdog time.Duration
}

// Options returns the service.Options that apply r. It always begins with
// service.WithContainer reporting false, because the driver runs inside the
// container whose service it manages and New otherwise refuses with a
// *service.ContainerError.
func (r Request) Options() []service.Option {
	opts := []service.Option{service.WithContainer(func() bool { return false })}
	if r.Identity != nil {
		opts = append(opts, service.WithIdentity(*r.Identity))
	}
	if r.Hardening != nil {
		opts = append(opts, service.WithSystemdHardening(*r.Hardening))
	}
	if r.Notify != nil {
		opts = append(opts, service.WithSystemdNotify(r.Notify.Watchdog))
	}
	return opts
}
