//go:build !windows

package daemon

import (
	"errors"
	"runtime"
	"testing"
)

func TestHostRunWindowsServiceFailsOffWindows(t *testing.T) {
	err := hostRunWindowsService(func(<-chan scmRequest, func(scmStatus)) uint32 {
		t.Error("execute called on a host with no service control manager")
		return 0
	})
	if err == nil {
		t.Fatal("hostRunWindowsService = nil, want an error")
	}
	if !errors.Is(err, errNotWindowsService) {
		t.Errorf("err = %v, want errNotWindowsService", err)
	}
	if want := "daemon: not running as a Windows service: no service control manager on " + runtime.GOOS; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}
