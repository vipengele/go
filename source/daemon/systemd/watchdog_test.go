package systemd

import (
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWatchdogInterval(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	other := strconv.Itoa(os.Getpid() + 1)
	tests := []struct {
		name    string
		env     env
		want    time.Duration
		enabled bool
	}{
		{"absent", env{}, 0, false},
		{"empty", env{"WATCHDOG_USEC": ""}, 0, false},
		{"usec", env{"WATCHDOG_USEC": "30000000"}, 30 * time.Second, true},
		{"one microsecond", env{"WATCHDOG_USEC": "1"}, time.Microsecond, true},
		{"own pid", env{"WATCHDOG_USEC": "4000000", "WATCHDOG_PID": self}, 4 * time.Second, true},
		{"empty pid", env{"WATCHDOG_USEC": "4000000", "WATCHDOG_PID": ""}, 4 * time.Second, true},
		{"other pid", env{"WATCHDOG_USEC": "4000000", "WATCHDOG_PID": other}, 0, false},
		{"other pid, malformed usec", env{"WATCHDOG_USEC": "soon", "WATCHDOG_PID": other}, 0, false},
		{"own pid, no usec", env{"WATCHDOG_PID": self}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, enabled, err := watchdogInterval("linux", tt.env.lookup)
			if err != nil || got != tt.want || enabled != tt.enabled {
				t.Errorf("watchdogInterval = %v, %v, %v; want %v, %v, nil", got, enabled, err, tt.want, tt.enabled)
			}
		})
	}
}

func TestWatchdogIntervalRejectsMalformedValues(t *testing.T) {
	for name, e := range map[string]env{
		"usec not a number":   {"WATCHDOG_USEC": "soon"},
		"usec zero":           {"WATCHDOG_USEC": "0"},
		"usec negative":       {"WATCHDOG_USEC": "-5"},
		"usec fractional":     {"WATCHDOG_USEC": "1.5"},
		"usec overflows":      {"WATCHDOG_USEC": "9223372036854775807"},
		"usec past int64":     {"WATCHDOG_USEC": "99999999999999999999"},
		"pid not a number":    {"WATCHDOG_USEC": "1000", "WATCHDOG_PID": "me"},
		"pid zero":            {"WATCHDOG_USEC": "1000", "WATCHDOG_PID": "0"},
		"pid negative":        {"WATCHDOG_USEC": "1000", "WATCHDOG_PID": "-1"},
		"pid with whitespace": {"WATCHDOG_USEC": "1000", "WATCHDOG_PID": " 1"},
	} {
		t.Run(name, func(t *testing.T) {
			got, enabled, err := watchdogInterval("linux", e.lookup)
			if err == nil || got != 0 || enabled {
				t.Errorf("watchdogInterval = %v, %v, %v; want 0, false and an error", got, enabled, err)
			}
		})
	}
}

func TestWatchdogIntervalIsDisabledOffLinux(t *testing.T) {
	got, enabled, err := watchdogInterval("darwin", untouched(t))
	if err != nil || got != 0 || enabled {
		t.Errorf("watchdogInterval = %v, %v, %v; want 0, false, nil", got, enabled, err)
	}
}

func TestWatchdogIntervalOutsideSystemdThroughTheExportedFunction(t *testing.T) {
	got, enabled, err := WatchdogInterval(env{}.lookup)
	if err != nil || got != 0 || enabled {
		t.Errorf("WatchdogInterval = %v, %v, %v; want 0, false, nil", got, enabled, err)
	}
}

func TestWatchdogIntervalAcceptsTheLargestDuration(t *testing.T) {
	const largest = math.MaxInt64 / int64(time.Microsecond)

	got, enabled, err := watchdogInterval("linux", env{"WATCHDOG_USEC": strconv.FormatInt(largest, 10)}.lookup)
	if want := time.Duration(largest) * time.Microsecond; err != nil || got != want || !enabled {
		t.Errorf("watchdogInterval = %v, %v, %v; want %v, true, nil", got, enabled, err, want)
	}

	got, enabled, err = watchdogInterval("linux", env{"WATCHDOG_USEC": strconv.FormatInt(largest+1, 10)}.lookup)
	if err == nil || got != 0 || enabled {
		t.Fatalf("watchdogInterval past the largest = %v, %v, %v; want 0, false and an error", got, enabled, err)
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want WATCHDOG_USEC rejected as too large", err)
	}
}

func TestParsePIDReturnsZeroWithItsError(t *testing.T) {
	for _, raw := range []string{"", "abc", "0", "-1"} {
		pid, err := parsePID("WATCHDOG_PID", raw)
		if err == nil || pid != 0 {
			t.Errorf("parsePID(%q) = %d, %v; want 0 and an error", raw, pid, err)
		}
	}
}
