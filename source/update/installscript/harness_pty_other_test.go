//go:build !linux

package installscript_test

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// attachPTY skips the test: the harness opens a pseudo-terminal on linux only.
func attachPTY(t *testing.T, _ *exec.Cmd) (master, slave *os.File, eof byte) {
	t.Helper()
	t.Skipf("the harness opens no terminal on %s", runtime.GOOS)
	return nil, nil, 0
}

// detachTerminal leaves cmd in the test's session: off linux the script shares the test's
// controlling terminal, if it has one.
func detachTerminal(_ *exec.Cmd) {}
