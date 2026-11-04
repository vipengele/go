package service

import (
	"fmt"
	"strconv"
)

// State is the lifecycle state of a service as the service manager reports it.
type State int

const (
	// StateUnknown is the zero value: the state could not be determined.
	StateUnknown State = iota
	// StateNotInstalled means the service manager has no such service.
	StateNotInstalled
	// StateStopped means the service is installed and not running.
	StateStopped
	// StateStarting means the service is activating and has not yet settled.
	StateStarting
	// StateRunning means the service is up.
	StateRunning
	// StateFailed means the service stopped abnormally or keeps restarting.
	StateFailed
)

// String returns the lower-case name of s, or "State(n)" for a value outside
// the defined set.
func (s State) String() string {
	switch s {
	case StateUnknown:
		return "unknown"
	case StateNotInstalled:
		return "not_installed"
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateFailed:
		return "failed"
	default:
		return "State(" + strconv.Itoa(int(s)) + ")"
	}
}

// MarshalText implements encoding.TextMarshaler. It fails for a value outside
// the defined set.
func (s State) MarshalText() ([]byte, error) {
	switch s {
	case StateUnknown, StateNotInstalled, StateStopped, StateStarting, StateRunning, StateFailed:
		return []byte(s.String()), nil
	default:
		return nil, fmt.Errorf("service: invalid state %d", int(s))
	}
}

// UnmarshalText implements encoding.TextUnmarshaler. It accepts exactly the
// names String returns for the defined values and rejects anything else,
// leaving s unchanged.
func (s *State) UnmarshalText(text []byte) error {
	var v State
	switch string(text) {
	case "unknown":
		v = StateUnknown
	case "not_installed":
		v = StateNotInstalled
	case "stopped":
		v = StateStopped
	case "starting":
		v = StateStarting
	case "running":
		v = StateRunning
	case "failed":
		v = StateFailed
	default:
		return fmt.Errorf("service: unknown state %q", text)
	}
	*s = v
	return nil
}

// Status is a snapshot of a service as the service manager reports it.
type Status struct {
	// State is the lifecycle state.
	State State `json:"state"`
	// Enabled reports whether the service is configured to start at boot or
	// login.
	Enabled bool `json:"enabled"`
	// PID is the process id of the running service, or 0 when there is none.
	PID int `json:"pid"`
	// Restarts is the number of times the service manager has restarted the
	// service.
	Restarts int `json:"restarts"`
	// Detail is free-form text from the service manager, such as the last
	// failure reason.
	Detail string `json:"detail"`
}

// resolve maps the state a driver observed to the state to report. Drivers
// report an activating service as StateStarting, never StateRunning; a start
// that has already restarted is a failure, so StateStarting with restarts
// above zero becomes StateFailed. Every other state is returned unchanged.
func resolve(observed State, restarts int) State {
	if observed == StateStarting && restarts > 0 {
		return StateFailed
	}
	return observed
}
