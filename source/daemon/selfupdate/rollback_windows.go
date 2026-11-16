package selfupdate

// platformRestore moves the target aside before it renames the backup into place: Windows lets the
// image of a running process be renamed but not replaced.
func platformRestore(ops fsOps, rec Record) error {
	return restoreAside(ops, rec)
}
