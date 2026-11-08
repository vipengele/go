package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.vipengele.org/daemon/systemd"
)

// sdEnv is a lookup backed by a map, the environment a test hands to WithSystemd.
type sdEnv map[string]string

func (e sdEnv) lookup(key string) (string, bool) {
	v, ok := e[key]
	return v, ok
}

// sdSocket listens on a unixgram socket, as systemd does on NOTIFY_SOCKET, in a short directory
// removed when the test ends, so that the path fits a socket address. It returns the socket's
// path and the listening end.
func sdSocket(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no unixgram sockets on windows")
	}
	dir, err := os.MkdirTemp("", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen %q: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return path, conn
}

// sdReceive fails the test unless the next datagram on conn, within a few seconds, is want.
func sdReceive(t *testing.T, conn *net.UnixConn, want string) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read datagram: %v, want %q", err, want)
	}
	if got := string(buf[:n]); got != want {
		t.Errorf("datagram = %q, want %q", got, want)
	}
}

// sdSilent fails the test when a datagram is waiting on conn. Every send it guards against is
// made before the call that would cause it returns, so a short wait is enough.
func sdSilent(t *testing.T, conn *net.UnixConn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err == nil {
		t.Errorf("datagram %q sent, want none", buf[:n])
		return
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read datagram: %v", err)
	}
}

// systemdRecords returns the records in log about the systemd notification protocol.
func systemdRecords(log *capturedLog) []logged {
	var out []logged
	for _, r := range log.all() {
		if strings.HasPrefix(r.msg, "systemd ") {
			out = append(out, r)
		}
	}
	return out
}

// checkSystemdWarning fails the test unless records is one Warn record msg carrying an error.
func checkSystemdWarning(t *testing.T, records []logged, msg string) {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("systemd records = %v, want one %q", records, msg)
	}
	r := records[0]
	if r.level != slog.LevelWarn || r.msg != msg {
		t.Errorf("record = %v %q, want %v %q", r.level, r.msg, slog.LevelWarn, msg)
	}
	if v, ok := r.attrs["err"]; !ok {
		t.Errorf("record %q: no err attribute", r.msg)
	} else if _, isErr := v.Any().(error); !isErr {
		t.Errorf("record %q: err = %v (%T), want an error", r.msg, v.Any(), v.Any())
	}
}

func TestNewRejectsANilSystemdLookup(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{name: "a"}}, WithSystemd(nil))
	if err == nil {
		t.Fatal("New with a nil systemd lookup succeeded")
	}
	if s != nil {
		t.Error("New returned a Supervisor alongside an error")
	}
}

func TestSystemdReadyIsSentAfterEveryStartHasReturned(t *testing.T) {
	path, conn := sdSocket(t)
	a, b := newScripted("a"), newScripted("b")
	b.hangStart = true
	h := launchRun(t, []Option{WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)}, a, b)

	<-a.started
	<-b.started
	sdSilent(t, conn)

	b.free()
	<-h.waiting
	sdReceive(t, conn, systemd.Ready)
	sdSilent(t, conn)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestSystemdStoppingIsSentAtShutdown(t *testing.T) {
	path, conn := sdSocket(t)
	h := runScriptedWith(t, []Option{WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)}, newScripted("a"))
	sdReceive(t, conn, systemd.Ready)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdReceive(t, conn, systemd.Stopping)
	sdSilent(t, conn)
	h.finish()
}

func TestSystemdStoppingWithoutReadyWhenAStartFails(t *testing.T) {
	path, conn := sdSocket(t)
	boom := errors.New("boom")
	a, b := newScripted("a"), newScripted("b")
	b.startErr = boom
	h := launchRun(t, []Option{WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)}, a, b)

	if err := <-h.result; !errors.Is(err, boom) {
		t.Errorf("Run = %v, want the start error %v", err, boom)
	}
	sdReceive(t, conn, systemd.Stopping)
	sdSilent(t, conn)
	h.finish()
}

func TestSystemdCoexistsWithReady(t *testing.T) {
	path, conn := sdSocket(t)
	var n atomic.Int32
	h := runScriptedWith(t, []Option{countReady(&n), WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)},
		newScripted("a"))
	checkReadyCalls(t, &n, 1)
	sdReceive(t, conn, systemd.Ready)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	checkReadyCalls(t, &n, 1)
	sdReceive(t, conn, systemd.Stopping)
	h.finish()
}

func TestSystemdWatchdogPingsEveryHalfInterval(t *testing.T) {
	path, conn := sdSocket(t)
	clock := newManualClock()
	env := sdEnv{"NOTIFY_SOCKET": path, "WATCHDOG_USEC": "2000000"}
	h := runScriptedWith(t, []Option{WithClock(clock), WithSystemd(env.lookup)}, newScripted("a"))
	sdReceive(t, conn, systemd.Ready)
	if got := clock.pending(); got != 1 {
		t.Fatalf("%d timers pending, want the watchdog's", got)
	}

	clock.Advance(time.Second - time.Millisecond)
	sdSilent(t, conn)
	clock.Advance(time.Millisecond)
	sdReceive(t, conn, systemd.Watchdog)
	clock.Advance(time.Second)
	sdReceive(t, conn, systemd.Watchdog)
	sdSilent(t, conn)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	h.finish()
}

func TestSystemdWatchdogStopsPingingAtStopping(t *testing.T) {
	path, conn := sdSocket(t)
	clock := newManualClock()
	env := sdEnv{"NOTIFY_SOCKET": path, "WATCHDOG_USEC": "2000000"}
	h := runScriptedWith(t, []Option{WithClock(clock), WithSystemd(env.lookup)}, newScripted("a"))
	sdReceive(t, conn, systemd.Ready)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdReceive(t, conn, systemd.Stopping)
	awaitNoTimers(t, clock)
	clock.Advance(time.Hour)
	sdSilent(t, conn)
	h.finish()
}

func TestSystemdWatchdogDoesNotPingWhileLivenessFails(t *testing.T) {
	path, conn := sdSocket(t)
	clock := newManualClock()
	var alive atomic.Bool
	health := NewHealth()
	err := health.Add("heart", func(context.Context) error {
		if !alive.Load() {
			return errors.New("no pulse")
		}
		return nil
	}, Liveness)
	if err != nil {
		t.Fatal(err)
	}
	env := sdEnv{"NOTIFY_SOCKET": path, "WATCHDOG_USEC": "2000000"}
	h := runScriptedWith(t, []Option{WithClock(clock), WithHealth(health), WithSystemd(env.lookup)},
		newScripted("a"))
	sdReceive(t, conn, systemd.Ready)

	for range 3 {
		clock.Advance(time.Second)
		sdSilent(t, conn)
	}
	alive.Store(true)
	clock.Advance(time.Second)
	sdReceive(t, conn, systemd.Watchdog)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdReceive(t, conn, systemd.Stopping)
	h.finish()
}

func TestSystemdWatchdogTickRunningAtDisarmNeitherPingsNorRearms(t *testing.T) {
	clock := newManualClock()
	var w watchdog
	checking, release := make(chan struct{}), make(chan struct{})
	var pings atomic.Int32
	w.arm(clock, time.Second, func() bool {
		close(checking)
		<-release
		return true
	}, func() { pings.Add(1) })

	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		clock.Advance(time.Second)
	}()
	<-checking
	w.disarm()
	close(release)
	<-ticked

	if got := pings.Load(); got != 0 {
		t.Errorf("pinged %d times after disarm, want none", got)
	}
	if got := clock.pending(); got != 0 {
		t.Errorf("%d timers pending after disarm, want none", got)
	}
}

func TestSystemdWatchdogDisarmBeforeArmIsANoOp(t *testing.T) {
	clock := newManualClock()
	var w watchdog
	w.disarm()
	w.arm(clock, time.Second, func() bool { return true }, func() { t.Error("pinged after disarm") })
	if got := clock.pending(); got != 0 {
		t.Errorf("%d timers pending, want none", got)
	}
}

func TestSystemdSendsNothingOutsideSystemd(t *testing.T) {
	_, conn := sdSocket(t)
	clock := newManualClock()
	capture, logger := newCapture()
	env := sdEnv{"WATCHDOG_USEC": "2000000"}
	h := runScriptedWith(t, []Option{WithClock(clock), WithLogger(logger), WithSystemd(env.lookup)},
		newScripted("a"))

	clock.Advance(time.Hour)
	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdSilent(t, conn)
	if got := systemdRecords(capture); len(got) != 0 {
		t.Errorf("systemd records = %v, want none", got)
	}
	h.finish()
}

func TestSystemdMalformedWatchdogIsLoggedAndSendsNoPing(t *testing.T) {
	path, conn := sdSocket(t)
	clock := newManualClock()
	capture, logger := newCapture()
	env := sdEnv{"NOTIFY_SOCKET": path, "WATCHDOG_USEC": "soon"}
	h := runScriptedWith(t, []Option{WithClock(clock), WithLogger(logger), WithSystemd(env.lookup)},
		newScripted("a"))
	sdReceive(t, conn, systemd.Ready)
	if got := clock.pending(); got != 0 {
		t.Errorf("%d timers pending, want no watchdog", got)
	}
	checkSystemdWarning(t, systemdRecords(capture), msgWatchdogDisabled)

	clock.Advance(time.Hour)
	sdSilent(t, conn)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	sdReceive(t, conn, systemd.Stopping)
	h.finish()
}

func TestSystemdNotifyFailureIsLoggedAndDoesNotFailRun(t *testing.T) {
	path, conn := sdSocket(t)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	capture, logger := newCapture()
	h := runScriptedWith(t, []Option{WithLogger(logger), WithSystemd(sdEnv{"NOTIFY_SOCKET": path}.lookup)},
		newScripted("a"))
	checkSystemdWarning(t, systemdRecords(capture), msgNotifyFailed)

	h.cancelRun()
	if err := <-h.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	records := systemdRecords(capture)
	if len(records) != 2 {
		t.Fatalf("systemd records = %v, want one per notification", records)
	}
	for i, want := range []string{systemd.Ready, systemd.Stopping} {
		checkSystemdWarning(t, records[i:i+1], msgNotifyFailed)
		if got := records[i].attrs["state"].String(); got != want {
			t.Errorf("record %d state = %q, want %q", i, got, want)
		}
	}
	h.finish()
}
