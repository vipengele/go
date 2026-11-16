package daemon

import "strconv"

// UnderSupervisor reports whether the process runs under a service supervisor that started it as
// the service named label. lookup reads the environment like os.LookupEnv, pid is the process's
// own pid, and no code here reads the environment itself. It is true when any of these holds:
//
//   - systemd: SYSTEMD_EXEC_PID equals pid. systemd sets it only on the service's main process,
//     so a child shell does not inherit a match. Before systemd 248 it is not set and the result
//     is false.
//   - launchd: XPC_SERVICE_NAME equals label. A process started from a terminal carries the
//     terminal's label, not the service's.
//   - Windows: the process runs under the service control manager. An error from that check
//     counts as not supervised.
//
// The result is exact, not a guess from the terminal or the parent process. A false negative
// leaves the caller to show a warning it could have skipped.
func UnderSupervisor(lookup func(string) (string, bool), pid int, label string) bool {
	return underSupervisor(lookup, pid, label, hostIsWindowsService)
}

// underSupervisor is UnderSupervisor with the Windows service check passed in.
func underSupervisor(lookup func(string) (string, bool), pid int, label string, isWindowsService func() (bool, error)) bool {
	if v, ok := lookup("SYSTEMD_EXEC_PID"); ok && v == strconv.Itoa(pid) {
		return true
	}
	if v, ok := lookup("XPC_SERVICE_NAME"); ok && label != "" && v == label {
		return true
	}
	ok, err := isWindowsService()
	return err == nil && ok
}
