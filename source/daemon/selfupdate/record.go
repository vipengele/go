package selfupdate

import (
	"fmt"
	"time"
)

// Status is the stage of a self-update. The zero Status is StatusIdle.
//
// It is stored by name, "idle", "pending", "confirmed" or "rolled_back". Any other name does not
// decode, so a record holding a status this build does not know is corrupt rather than idle.
type Status string

const (
	// StatusIdle means no update is in progress: none was begun, or the last one is settled and
	// forgotten. It is the zero Status.
	StatusIdle Status = ""
	// StatusPending means a new binary is installed and has not yet been confirmed.
	StatusPending Status = "pending"
	// StatusConfirmed means the updated build was confirmed and its backup released.
	StatusConfirmed Status = "confirmed"
	// StatusRolledBack means the backup was restored over the target.
	StatusRolledBack Status = "rolled_back"
)

// idleName is the stored name of StatusIdle.
const idleName = "idle"

// String returns the status's stored name: "idle" for the zero Status.
func (s Status) String() string {
	if s == StatusIdle {
		return idleName
	}

	return string(s)
}

// MarshalText returns the status's stored name. A Status outside the four is an error.
func (s Status) MarshalText() ([]byte, error) {
	switch s {
	case StatusIdle, StatusPending, StatusConfirmed, StatusRolledBack:
		return []byte(s.String()), nil
	default:
		return nil, fmt.Errorf("selfupdate: unknown status %q", string(s))
	}
}

// UnmarshalText reads a stored name. "idle" and the empty name are StatusIdle; any name but the
// four is an error.
func (s *Status) UnmarshalText(text []byte) error {
	switch name := string(text); name {
	case idleName, "":
		*s = StatusIdle
	case string(StatusPending), string(StatusConfirmed), string(StatusRolledBack):
		*s = Status(name)
	default:
		return fmt.Errorf("selfupdate: unknown status %q", name)
	}

	return nil
}

// Record is the stored state of a self-update. The zero Record is idle.
//
// It is stored as JSON with the keys "status", always written, and "from_version", "to_version",
// "target", "backup", "boot_attempts", "reason", "started_at" and "updated_at", each omitted when
// zero.
type Record struct {
	// Status is the update's stage.
	Status Status `json:"status"`
	// FromVersion is the version that was running when the update began.
	FromVersion string `json:"from_version,omitzero"`
	// ToVersion is the version the update installed.
	ToVersion string `json:"to_version,omitzero"`
	// Target is the path of the installed binary. It is stored so that a process other than the
	// one that installed it can roll back.
	Target string `json:"target,omitzero"`
	// Backup is the path of the previous binary, which a rollback renames over Target.
	Backup string `json:"backup,omitzero"`
	// BootAttempts counts the boots of the updated build while the record is pending.
	BootAttempts int `json:"boot_attempts,omitzero"`
	// Reason says why the record reached its Status, such as the cause of a rollback.
	Reason string `json:"reason,omitzero"`
	// StartedAt is when the update began.
	StartedAt time.Time `json:"started_at,omitzero"`
	// UpdatedAt is when the record last changed.
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}
