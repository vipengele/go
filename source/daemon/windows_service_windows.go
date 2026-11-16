//go:build windows

package daemon

import (
	"sync"

	"golang.org/x/sys/windows/svc"
)

// hostIsWindowsService reports whether the process runs under the Windows service control
// manager.
func hostIsWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// hostRunWindowsService serves execute as the process's service through svc.Run and returns once
// the service has stopped. The name is empty: the SCM ignores it for a service that runs in its
// own process.
func hostRunWindowsService(execute scmExecute) error {
	return svc.Run("", svcHandler{execute: execute})
}

// svcHandler is the svc.Handler that translates between the svc types and those of execute.
type svcHandler struct {
	execute scmExecute
}

// Execute serves the service with h.execute. A goroutine forwards each control from r to execute
// until execute has returned, and each status execute reports is sent on s before report
// returns. The exit code is returned as service-specific: svc reports exit code 0 as NO_ERROR,
// whatever svcSpecificEC says, and a non-zero code as ERROR_SERVICE_SPECIFIC_ERROR carrying it.
// A Stopped status sent on s would report exit code 0, so execute never reports one: svc reports
// it with the returned code once Execute returns.
func (h svcHandler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	requests := make(chan scmRequest)
	quit := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case c := <-r:
				select {
				case requests <- scmRequest{Cmd: scmCmd(c.Cmd)}:
				case <-quit:
					return
				}
			case <-quit:
				return
			}
		}
	})

	code := h.execute(requests, func(st scmStatus) {
		s <- svc.Status{State: svc.State(st.State), Accepts: svc.Accepted(st.Accepts), WaitHint: st.WaitHint}
	})
	close(quit)
	wg.Wait()
	return code != 0, code
}
