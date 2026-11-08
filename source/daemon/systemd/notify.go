package systemd

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"time"
)

// notifyTimeout bounds the write of a Notify datagram. A receiver that has stopped reading fills
// the socket buffer, and a write without a deadline would then block its caller indefinitely.
const notifyTimeout = time.Second

// The states a daemon sends with Notify. Each is one newline-free assignment, as systemd reads
// it from the datagram.
const (
	// Ready tells systemd that the service has finished starting. A unit of Type=notify is
	// active only once it is received.
	Ready = "READY=1"

	// Stopping tells systemd that the service has begun to shut down.
	Stopping = "STOPPING=1"

	// Watchdog tells systemd that the service is alive. A unit with WatchdogSec= that receives
	// none within the interval is restarted.
	Watchdog = "WATCHDOG=1"
)

// Notify sends state to the service manager as one datagram on the unixgram socket named by
// NOTIFY_SOCKET, which lookup reads.
//
// It reports sent as false, with a nil error, when runtime.GOOS is not linux or NOTIFY_SOCKET is
// absent or empty: the process is not run by a systemd that listens for notifications, and
// there is nobody to tell. A NOTIFY_SOCKET starting with @ names a socket in the abstract
// namespace. A NOTIFY_SOCKET that is neither an absolute path nor an abstract name, an empty
// state, and a failure to reach the socket or write to it are errors, with sent false. A write
// that cannot complete within one second, because the receiver has stopped reading and the socket
// buffer is full, fails with an error matching os.ErrDeadlineExceeded.
func Notify(lookup func(key string) (string, bool), state string) (sent bool, err error) {
	return notify(runtime.GOOS, notifyTimeout, lookup, state)
}

// notify is Notify for the operating system goos, with timeout bounding the write.
func notify(goos string, timeout time.Duration, lookup func(key string) (string, bool), state string) (bool, error) {
	if goos != "linux" {
		return false, nil
	}
	name, ok := lookup("NOTIFY_SOCKET")
	if !ok || name == "" {
		return false, nil
	}
	if state == "" {
		return false, errors.New("systemd: notify: empty state")
	}
	switch name[0] {
	case '/':
	case '@':
		name = "\x00" + name[1:]
	default:
		return false, fmt.Errorf("systemd: notify: NOTIFY_SOCKET %q is neither an absolute path nor an abstract name", name)
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		return false, fmt.Errorf("systemd: notify: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return false, fmt.Errorf("systemd: notify: %w", err)
	}
	if _, err := conn.Write([]byte(state)); err != nil {
		_ = conn.Close()
		return false, fmt.Errorf("systemd: notify: %w", err)
	}
	if err := conn.Close(); err != nil {
		return false, fmt.Errorf("systemd: notify: %w", err)
	}
	return true, nil
}
