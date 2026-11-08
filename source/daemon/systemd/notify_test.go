package systemd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// env is a lookup backed by a map, the environment a test hands to the package.
type env map[string]string

func (e env) lookup(key string) (string, bool) {
	v, ok := e[key]
	return v, ok
}

// untouched is a lookup that fails the test when it is called.
func untouched(t *testing.T) func(string) (string, bool) {
	t.Helper()
	return func(key string) (string, bool) {
		t.Errorf("lookup(%q) called", key)
		return "", false
	}
}

// listenNotify listens on a unixgram socket at name, as systemd does on NOTIFY_SOCKET, and
// closes it when the test ends.
func listenNotify(t *testing.T, name string) *net.UnixConn {
	t.Helper()
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen %q: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// socketPath returns a path for a unix socket in a directory removed when the test ends. The
// directory is short, so that the path stays within the length limit of a socket address.
func socketPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no unixgram sockets on windows")
	}
	dir, err := os.MkdirTemp("", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "notify")
}

// receive reads one datagram from conn.
func receive(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read datagram: %v", err)
	}
	return string(buf[:n])
}

func TestNotifySendsTheStateAsOneDatagram(t *testing.T) {
	path := socketPath(t)
	conn := listenNotify(t, path)

	for _, state := range []string{Ready, Watchdog, Stopping} {
		sent, err := notify("linux", notifyTimeout, env{"NOTIFY_SOCKET": path}.lookup, state)
		if err != nil || !sent {
			t.Fatalf("notify(%q) = %v, %v; want true, nil", state, sent, err)
		}
		if got := receive(t, conn); got != state {
			t.Errorf("datagram = %q, want %q", got, state)
		}
	}
}

func TestNotifyMapsAnAtToTheAbstractNamespace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstract unix sockets exist on linux only")
	}
	name := fmt.Sprintf("vipengele-systemd-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	conn := listenNotify(t, "\x00"+name)

	sent, err := notify("linux", notifyTimeout, env{"NOTIFY_SOCKET": "@" + name}.lookup, Ready)
	if err != nil || !sent {
		t.Fatalf("notify = %v, %v; want true, nil", sent, err)
	}
	if got := receive(t, conn); got != Ready {
		t.Errorf("datagram = %q, want %q", got, Ready)
	}
}

func TestNotifyIsANoOpOutsideSystemd(t *testing.T) {
	for name, e := range map[string]env{
		"absent": {},
		"empty":  {"NOTIFY_SOCKET": ""},
	} {
		t.Run(name, func(t *testing.T) {
			sent, err := notify("linux", notifyTimeout, e.lookup, Ready)
			if err != nil || sent {
				t.Errorf("notify = %v, %v; want false, nil", sent, err)
			}
		})
	}
}

func TestNotifyIsANoOpOffLinux(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "freebsd"} {
		sent, err := notify(goos, notifyTimeout, untouched(t), Ready)
		if err != nil || sent {
			t.Errorf("notify on %s = %v, %v; want false, nil", goos, sent, err)
		}
	}
}

func TestNotifyOutsideSystemdThroughTheExportedFunction(t *testing.T) {
	sent, err := Notify(env{}.lookup, Ready)
	if err != nil || sent {
		t.Errorf("Notify = %v, %v; want false, nil", sent, err)
	}
}

func TestNotifyRejectsASocketThatIsNeitherAPathNorAbstract(t *testing.T) {
	sent, err := notify("linux", notifyTimeout, env{"NOTIFY_SOCKET": "run/notify"}.lookup, Ready)
	if err == nil || sent {
		t.Errorf("notify = %v, %v; want false and an error", sent, err)
	}
}

func TestNotifyRejectsAnEmptyState(t *testing.T) {
	sent, err := notify("linux", notifyTimeout, env{"NOTIFY_SOCKET": "/run/systemd/notify"}.lookup, "")
	if err == nil || sent {
		t.Errorf("notify = %v, %v; want false and an error", sent, err)
	}
}

func TestNotifyFailsWhenNothingListens(t *testing.T) {
	path := socketPath(t)

	sent, err := notify("linux", notifyTimeout, env{"NOTIFY_SOCKET": path}.lookup, Ready)
	if err == nil || sent {
		t.Fatalf("notify = %v, %v; want false and an error", sent, err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want one matching os.ErrNotExist", err)
	}
}

func TestNotifyTimesOutWhenTheReceiverStopsReading(t *testing.T) {
	path := socketPath(t)
	listenNotify(t, path)

	// The receiver never reads, so its buffer fills within a few hundred datagrams and the next
	// write blocks until its deadline.
	const maxSends = 5000
	for range maxSends {
		sent, err := notify("linux", 50*time.Millisecond, env{"NOTIFY_SOCKET": path}.lookup, Watchdog)
		if err == nil {
			continue
		}
		if sent {
			t.Errorf("notify = true, %v; want false with the error", err)
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("err = %v, want one matching os.ErrDeadlineExceeded", err)
		}
		return
	}
	t.Fatalf("%d sends to a receiver that never reads all succeeded", maxSends)
}
