package daemon

import (
	"io/fs"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
)

// AcquireInstanceFailingFiles is AcquireInstance with a rename and a remove of the holder record
// that always fail, as a Windows sharing violation makes them.
func AcquireInstanceFailingFiles(id spec.Identity, opts paths.Options, err error) (*Instance, error) {
	ops := osFileOps()
	ops.rename = func(string, string) error { return err }
	ops.remove = func(string) error { return err }

	return acquireInstance(id, opts, ops)
}

// AcquireInstanceFailingWrite is AcquireInstance with a write of the holder record that always
// fails.
func AcquireInstanceFailingWrite(id spec.Identity, opts paths.Options, err error) (*Instance, error) {
	ops := osFileOps()
	ops.writeFile = func(string, []byte, fs.FileMode) error { return err }

	return acquireInstance(id, opts, ops)
}
