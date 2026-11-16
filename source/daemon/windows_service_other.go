//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"runtime"
)

// errNotWindowsService is what hostRunWindowsService returns on a host with no Windows service
// control manager.
var errNotWindowsService = errors.New("daemon: not running as a Windows service")

// hostIsWindowsService reports that the process does not run under the Windows service control
// manager, which only Windows has.
func hostIsWindowsService() (bool, error) {
	return false, nil
}

// hostRunWindowsService fails with errNotWindowsService and never calls execute: only Windows has
// a service control manager to serve.
func hostRunWindowsService(scmExecute) error {
	return fmt.Errorf("%w: no service control manager on %s", errNotWindowsService, runtime.GOOS)
}
