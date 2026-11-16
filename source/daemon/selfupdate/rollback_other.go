//go:build !windows

package selfupdate

// platformRestore renames the backup over the target, which the file system allows while the
// target is the image of a running process.
func platformRestore(ops fsOps, rec Record) error {
	return restoreInPlace(ops, rec)
}
