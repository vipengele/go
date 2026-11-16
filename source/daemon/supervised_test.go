package daemon

import (
	"errors"
	"strconv"
	"testing"
)

func TestUnderSupervisor(t *testing.T) {
	const (
		pid   = 4242
		label = "org.example.tool"
	)
	tests := []struct {
		name string
		env  map[string]string
		win  func() (bool, error)
		want bool
	}{
		{name: "systemd pid equal", env: map[string]string{"SYSTEMD_EXEC_PID": strconv.Itoa(pid)}, want: true},
		{name: "systemd pid different", env: map[string]string{"SYSTEMD_EXEC_PID": "1"}, want: false},
		{name: "systemd pid unset", env: map[string]string{}, want: false},
		{name: "launchd label equal", env: map[string]string{"XPC_SERVICE_NAME": label}, want: true},
		{name: "launchd terminal label", env: map[string]string{"XPC_SERVICE_NAME": "com.apple.Terminal"}, want: false},
		{name: "launchd label unset", env: map[string]string{}, want: false},
		{name: "launchd empty label", env: map[string]string{"XPC_SERVICE_NAME": ""}, want: false},
		{name: "windows service", win: func() (bool, error) { return true, nil }, want: true},
		{name: "windows not a service", win: func() (bool, error) { return false, nil }, want: false},
		{name: "windows check error", win: func() (bool, error) { return true, errors.New("boom") }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(k string) (string, bool) {
				v, ok := tt.env[k]
				return v, ok
			}
			win := tt.win
			if win == nil {
				win = func() (bool, error) { return false, nil }
			}
			if got := underSupervisor(lookup, pid, label, win); got != tt.want {
				t.Errorf("underSupervisor = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnderSupervisorHostCheck(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	if UnderSupervisor(none, 1, "x") {
		t.Error("UnderSupervisor with no supervisor signals = true, want false")
	}
}
