package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// statusProperties are the unit properties Status asks systemctl show for.
const statusProperties = "LoadState,ActiveState,SubState,Result,MainPID,NRestarts,UnitFileState"

// Status reports the unit as `systemctl show` describes it, in one command.
// It needs no privileges. A unit with no unit file is StateNotInstalled and
// runs no command. A manager that cannot be reached yields StateUnknown with
// an error wrapping ErrManagerUnreachable. For a user unit whose user has
// lingering off, Detail says so: the service then stops when the user's last
// session ends.
func (s *systemd) Status(ctx context.Context) (Status, error) {
	if _, err := s.installedUnitPath(); err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{State: StateUnknown}, err
	}
	st, err := s.show(ctx)
	if note := s.lingerNote(); note != "" {
		st.Detail = joinDetail(st.Detail, note)
	}
	return st, err
}

// show runs `systemctl show` for the unit and maps its properties to a
// Status.
func (s *systemd) show(ctx context.Context) (Status, error) {
	unit := s.unitName()
	out, err := s.systemctl(ctx, "show", unit, "--property="+statusProperties)
	if err != nil {
		if errors.Is(err, ErrManagerUnreachable) {
			return Status{State: StateUnknown, Detail: s.unreachableDetail()},
				fmt.Errorf("service: status %s: %w", unit, err)
		}
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: %w", unit, err)
	}
	props := parseProperties(out)

	st := Status{
		State:   StateUnknown,
		Enabled: props["UnitFileState"] == "enabled" || props["UnitFileState"] == "enabled-runtime",
	}
	if load := props["LoadState"]; load != "loaded" {
		st.Detail = fmt.Sprintf("systemd reports LoadState=%q for %s", load, unit)
		return st, nil
	}
	if v, ok := props["MainPID"]; ok {
		pid, err := strconv.Atoi(v)
		if err != nil {
			st.Detail = fmt.Sprintf("systemd reports an unreadable MainPID=%q for %s", v, unit)
			return st, nil
		}
		st.PID = pid
	}
	// NRestarts exists from systemd 235 on; an older manager omits it.
	if v, ok := props["NRestarts"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			st.Detail = fmt.Sprintf("systemd reports an unreadable NRestarts=%q for %s", v, unit)
			return st, nil
		}
		st.Restarts = n
	}

	var observed State
	switch active := props["ActiveState"]; active {
	case "active", "reloading":
		observed = StateRunning
	case "activating":
		observed = StateStarting
	case "inactive", "deactivating":
		observed = StateStopped
	case "failed":
		observed = StateFailed
	default:
		st.Detail = fmt.Sprintf("systemd reports ActiveState=%q for %s", active, unit)
		return st, nil
	}
	// A unit restarting after a clean exit is still starting; restarts count
	// against it only when the last run did not succeed.
	restarts := st.Restarts
	result := props["Result"]
	if result == "success" {
		restarts = 0
	}
	st.State = resolve(observed, restarts)
	if st.State == StateFailed {
		st.Detail = fmt.Sprintf("%s failed (Result=%s, SubState=%s, %d restarts); see %s",
			unit, result, props["SubState"], st.Restarts, s.journalctl())
	}
	return st, nil
}

// unreachableDetail explains why the service manager could not be reached.
func (s *systemd) unreachableDetail() string {
	if s.user() {
		return "the user service manager is unreachable: a shell entered with sudo -u, " +
			"a cron job or any process outside a login session has no user bus"
	}
	return "the system service manager is unreachable"
}

// journalctl is the command that shows the unit's log.
func (s *systemd) journalctl() string {
	if s.user() {
		return "journalctl --user -u " + s.unitName()
	}
	return "journalctl -u " + s.unitName()
}

// lingerNote is the Detail for a user unit whose user has lingering off, or
// "" when lingering is on, the unit is a system unit, or the user name or the
// linger directory is unknown. The driver never turns lingering on.
func (s *systemd) lingerNote() string {
	if !s.user() || s.lingerDir == "" {
		return ""
	}
	name, ok := s.lookupEnv("USER")
	if !ok || name == "" {
		name, ok = s.lookupEnv("LOGNAME")
	}
	if !ok || name == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(s.lingerDir, name)); !errors.Is(err, os.ErrNotExist) {
		return ""
	}
	return fmt.Sprintf("lingering is off for %s: the service stops when the user's last session ends; "+
		"`loginctl enable-linger %s` turns it on", name, name)
}

// parseProperties reads the KEY=value lines of `systemctl show`. A line with
// no "=" is skipped; a value keeps any "=" it contains.
func parseProperties(out []byte) map[string]string {
	props := map[string]string{}
	for line := range strings.Lines(string(out)) {
		key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "=")
		if ok {
			props[key] = value
		}
	}
	return props
}

// joinDetail joins two Detail texts with "; ", dropping an empty one.
func joinDetail(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}
