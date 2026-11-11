package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Status reports the job as `launchctl print <domain>/<label>` describes it,
// and whether it is enabled as `launchctl print-disabled <domain>` lists it.
// It needs no privileges. A job with no plist is StateNotInstalled and runs
// no command; a plist launchd has not loaded (exit 113) is StateStopped. A
// gui domain that does not exist yields StateUnknown with an error wrapping
// ErrManagerUnreachable, and a missing launchctl an error wrapping
// ErrUnsupported. Output Status cannot read yields StateUnknown with a Detail
// and no error.
//
// A running job counts its earlier runs as restarts when its last run ended
// with a non-zero exit code or a signal. A loaded job with no process is
// StateStarting, which resolve turns into StateFailed when its last run
// failed and it has run more than once.
//
// When print-disabled fails, the Status keeps the state print reported,
// Enabled is false, Detail says the enabled state is unknown, and the error
// is returned with it.
func (l *launchd) Status(ctx context.Context) (Status, error) {
	if _, err := l.installedPlistPath(); err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{State: StateUnknown}, err
	}
	if err := l.checkDomain(ctx); err != nil {
		st := Status{State: StateUnknown}
		if errors.Is(err, ErrManagerUnreachable) {
			st.Detail = l.unreachableDetail()
		}
		return st, fmt.Errorf("service: status %s: %w", l.label(), err)
	}
	st, err := l.print(ctx)
	if err != nil {
		return st, err
	}
	enabled, err := l.enabled(ctx)
	if err != nil {
		st.Detail = joinDetail(st.Detail,
			fmt.Sprintf("launchctl print-disabled %s failed, so whether %s is enabled is unknown", l.domain(), l.label()))
		return st, fmt.Errorf("service: status %s: %w", l.label(), err)
	}
	st.Enabled = enabled
	return st, nil
}

// print runs `launchctl print` for the job and maps its top-level properties
// to a Status. Enabled is left false.
func (l *launchd) print(ctx context.Context) (Status, error) {
	target := l.target()
	out, err := l.launchctl(ctx, "print", target)
	if err != nil {
		if code, ok := exitCode(err); ok && code == launchctlNotFound {
			return Status{State: StateStopped}, nil
		}
		return Status{State: StateUnknown}, fmt.Errorf("service: status %s: print %s: %w", l.label(), target, err)
	}
	props := parseLaunchdPrint(out)

	st := Status{State: StateUnknown}
	state, ok := props["state"]
	if !ok || state == "" {
		st.Detail = fmt.Sprintf("launchctl print %s reports no state", target)
		return st, nil
	}
	pid := 0
	if v, ok := props["pid"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			st.Detail = fmt.Sprintf("launchctl print %s reports an unreadable pid = %q", target, v)
			return st, nil
		}
		pid = n
	}
	runs := 0
	if v, ok := props["runs"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			st.Detail = fmt.Sprintf("launchctl print %s reports an unreadable runs = %q", target, v)
			return st, nil
		}
		runs = n
	}
	failed, reason, ok := lastRunFailed(props)
	if !ok {
		st.Detail = fmt.Sprintf("launchctl print %s reports an unreadable last exit code = %q",
			target, props["last exit code"])
		return st, nil
	}

	// Every run after the first is a relaunch; relaunches count against the
	// job only when the last run did not succeed.
	restarts := 0
	if failed && runs > 1 {
		restarts = runs - 1
	}
	st.Restarts = restarts
	observed := StateStarting
	if state == "running" && pid > 0 {
		observed = StateRunning
		st.PID = pid
	}
	st.State = resolve(observed, restarts)
	if st.State == StateFailed {
		st.Detail = fmt.Sprintf("%s failed (%s, state = %s, %d restarts); see %s",
			target, reason, state, restarts, l.logHint())
	}
	return st, nil
}

// lastRunFailed reads how the job's last run ended from the `last exit code`
// and `last terminating signal` properties. failed is true for a non-zero
// exit code or a signal, and reason then names it; a job that has never
// exited has not failed. ok is false when the exit code is neither a number,
// optionally followed by ": <text>", nor a parenthesised note such as
// "(never exited)".
func lastRunFailed(props map[string]string) (failed bool, reason string, ok bool) {
	if sig := props["last terminating signal"]; sig != "" {
		return true, "last terminating signal = " + sig, true
	}
	v, present := props["last exit code"]
	if !present || strings.HasPrefix(v, "(") {
		return false, "", true
	}
	num, _, _ := strings.Cut(v, ":")
	code, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil {
		return false, "", false
	}
	if code == 0 {
		return false, "", true
	}
	return true, "last exit code = " + v, true
}

// parseLaunchdPrint reads the `key = value` lines of the service's own block
// in `launchctl print` output. The output opens with a `<target> = {` line;
// the service's properties are the lines one level inside it. Nested blocks,
// such as `environment = {` or `endpoints = {`, are skipped, so their keys
// never shadow the service's. Output with no opening block is read at the top
// level. Values keep any " = " they contain; the first occurrence of a key
// wins.
func parseLaunchdPrint(out []byte) map[string]string {
	props := map[string]string{}
	level := -1
	depth := 0
	for line := range strings.Lines(string(out)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if level < 0 {
			level = 0
			if strings.HasSuffix(trimmed, "{") {
				level = 1
				depth = 1
				continue
			}
		}
		switch {
		case strings.HasSuffix(trimmed, "{"):
			depth++
			continue
		case strings.HasPrefix(trimmed, "}"):
			depth--
			continue
		}
		if depth != level {
			continue
		}
		key, value, ok := strings.Cut(trimmed, " = ")
		if !ok {
			continue
		}
		if _, seen := props[key]; !seen {
			props[key] = strings.TrimSpace(value)
		}
	}
	return props
}

// enabled reports whether `launchctl print-disabled <domain>` lists the job
// as enabled.
func (l *launchd) enabled(ctx context.Context) (bool, error) {
	out, err := l.launchctl(ctx, "print-disabled", l.domain())
	if err != nil {
		return false, fmt.Errorf("print-disabled %s: %w", l.domain(), err)
	}
	return !launchdDisabled(out, l.label()), nil
}

// launchdDisabled reports whether `launchctl print-disabled` output lists
// label as disabled. It reads `"<label>" => disabled|enabled` lines and the
// `"<label>" => true|false` lines of older releases, where true means
// disabled. Lines inside a block other than `disabled services = {` are
// skipped. A label that is not listed is enabled.
func launchdDisabled(out []byte, label string) bool {
	quoted := strconv.Quote(label)
	depth := 0
	skipFrom := 0
	for line := range strings.Lines(string(out)) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasSuffix(trimmed, "{"):
			depth++
			if skipFrom == 0 && !strings.HasPrefix(trimmed, "disabled services") {
				skipFrom = depth
			}
			continue
		case strings.HasPrefix(trimmed, "}"):
			if depth == skipFrom {
				skipFrom = 0
			}
			depth--
			continue
		}
		if skipFrom != 0 {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=>")
		if !ok || strings.TrimSpace(key) != quoted {
			continue
		}
		switch strings.TrimSpace(value) {
		case "disabled", "true":
			return true
		default:
			return false
		}
	}
	return false
}

// unreachableDetail explains why the job's domain could not be reached.
func (l *launchd) unreachableDetail() string {
	return fmt.Sprintf("the launchd domain %s is unavailable: a LaunchAgent needs its user logged in to "+
		"the GUI, which an ssh login or a sudo -u shell is not", l.domain())
}

// logHint names the job's log file, or both files when stdout and stderr go
// to different paths.
func (l *launchd) logHint() string {
	logs, err := l.logPaths()
	if err != nil {
		return "the job's log files"
	}
	if logs.stdout == logs.stderr {
		return logs.stdout
	}
	return logs.stdout + " and " + logs.stderr
}
