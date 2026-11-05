package installscript_test

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// attachPTY opens a pseudo-terminal and makes its slave cmd's stderr and controlling terminal, in a
// session of its own, so the command reaches it as /dev/tty. It returns the master, the slave, which
// the caller closes once cmd has started, and the character that, at the start of a line written
// to the master, makes a read of the terminal return end of file. Both files are closed when the
// test ends; closing either earlier is harmless.
func attachPTY(t *testing.T, cmd *exec.Cmd) (master, slave *os.File, eof byte) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })

	// Fd would put the master into blocking mode, and Run relies on its read deadline.
	conn, err := master.SyscallConn()
	if err != nil {
		t.Fatalf("reaching the pty master: %v", err)
	}
	var n int
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		if ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioctlErr != nil {
			return
		}
		n, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
	}); err != nil {
		t.Fatalf("reaching the pty master: %v", err)
	}
	if ioctlErr != nil {
		t.Fatalf("unlocking the pty: %v", ioctlErr)
	}

	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening the pty slave: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })

	conn, err = slave.SyscallConn()
	if err != nil {
		t.Fatalf("reaching the pty slave: %v", err)
	}
	var termios *unix.Termios
	if err := conn.Control(func(fd uintptr) {
		termios, ioctlErr = unix.IoctlGetTermios(int(fd), unix.TCGETS)
	}); err != nil {
		t.Fatalf("reaching the pty slave: %v", err)
	}
	if ioctlErr != nil {
		t.Fatalf("reading the pty's termios: %v", ioctlErr)
	}
	cmd.Stderr = slave
	// Ctty is a descriptor number in the child, here its stderr.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 2}
	return master, slave, termios.Cc[unix.VEOF]
}

// detachTerminal starts cmd in a session of its own, so it has no controlling terminal even when
// the test has one, and opening /dev/tty fails as it does for `curl | sh` with no terminal.
func detachTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
