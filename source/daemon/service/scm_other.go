//go:build !windows

package service

import (
	"fmt"
	"runtime"
)

// hostSCM is the service control manager of the host, which only Windows
// has: every call fails with an error wrapping ErrUnsupported.
func hostSCM() scm {
	return unsupportedSCM{goos: runtime.GOOS}
}

// unsupportedSCM is the scm of a host other than Windows, goos.
type unsupportedSCM struct {
	goos string
}

func (u unsupportedSCM) elevated() (bool, error) {
	return false, u.err()
}

func (u unsupportedSCM) connect(scmAccess) (scmManager, error) {
	return nil, u.err()
}

func (u unsupportedSCM) err() error {
	return fmt.Errorf("%w: the Windows service control manager on %s", ErrUnsupported, u.goos)
}
