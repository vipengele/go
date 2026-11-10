package systemd

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"time"
)

// WatchdogInterval reports the watchdog interval systemd expects the service to keep, read from
// WATCHDOG_USEC and WATCHDOG_PID through lookup.
//
// The interval is the whole of WATCHDOG_USEC, the time after which systemd restarts a service
// that has sent no Watchdog. The service pings more often than that, conventionally at half the
// interval, so that a late ping still arrives in time.
//
// enabled is false, with a zero interval and a nil error, when runtime.GOOS is not linux, when
// WATCHDOG_USEC is absent or empty, and when WATCHDOG_PID is set to a process other than this one,
// which means the variables were meant for another process and inherited by this one. A
// WATCHDOG_PID that is not a positive integer is an error. WATCHDOG_PID is read first, so a
// variable meant for another process is never an error. A WATCHDOG_USEC that is not a positive
// integer number of microseconds, or does not fit a time.Duration, is an error.
func WatchdogInterval(lookup func(key string) (string, bool)) (interval time.Duration, enabled bool, err error) {
	return watchdogInterval(runtime.GOOS, lookup)
}

// watchdogInterval is WatchdogInterval for the operating system goos.
func watchdogInterval(goos string, lookup func(key string) (string, bool)) (time.Duration, bool, error) {
	if goos != "linux" {
		return 0, false, nil
	}
	if raw, ok := lookup("WATCHDOG_PID"); ok && raw != "" {
		pid, err := parsePID("WATCHDOG_PID", raw)
		if err != nil {
			return 0, false, err
		}
		if pid != os.Getpid() {
			return 0, false, nil
		}
	}
	raw, ok := lookup("WATCHDOG_USEC")
	if !ok || raw == "" {
		return 0, false, nil
	}
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || usec <= 0 {
		return 0, false, fmt.Errorf("systemd: WATCHDOG_USEC %q is not a positive integer", raw)
	}
	if usec > math.MaxInt64/int64(time.Microsecond) {
		return 0, false, fmt.Errorf("systemd: WATCHDOG_USEC %q is too large", raw)
	}
	return time.Duration(usec) * time.Microsecond, true, nil
}

// parsePID parses raw, the value of the variable key, as a process id.
func parsePID(key, raw string) (int, error) {
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("systemd: %s %q is not a positive integer", key, raw)
	}
	return pid, nil
}
