package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/filelock"
)

const (
	lockName      = "instance.lock"
	holderName    = "instance.holder"
	holderTmpName = "instance.holder.tmp"

	holderPerm fs.FileMode = 0o600
)

// fileOps are the file system calls on the holder record that can fail without failing the
// acquire.
type fileOps struct {
	writeFile func(name string, data []byte, perm fs.FileMode) error
	rename    func(oldpath, newpath string) error
	remove    func(name string) error
}

func osFileOps() fileOps {
	return fileOps{writeFile: os.WriteFile, rename: os.Rename, remove: os.Remove}
}

// Instance is the single running instance of a tool on one machine for one runtime directory. It
// holds the instance lock until Release.
type Instance struct {
	path       string
	holderPath string
	ops        fileOps
	unlock     func()
	once       sync.Once
}

// AcquireInstance takes the instance lock in the runtime directory of the tool id, which it
// creates when missing, and records the process id of the caller beside the lock.
//
// When another instance holds the lock the error is an *AlreadyRunningError, which matches
// ErrAlreadyRunning and carries the holder's pid when its record can be read. Any other failure to
// resolve the directory, create it or take the lock is returned wrapped.
//
// The lock alone decides which instance runs; the record only describes the holder, so writing it
// is best effort and a failure never fails the acquire. Three properties follow from that:
//
//   - A loser can read the previous holder's pid between a winner's acquire and its rewrite of the
//     record.
//   - On Windows Go opens files without FILE_SHARE_DELETE, so a winner's rename or remove can fail
//     while a loser reads the record, leaving the previous pid in place.
//   - The temporary file has a fixed name, so a crash leaves at most one stray instance.holder.tmp,
//     which the next write overwrites.
func AcquireInstance(id spec.Identity, opts paths.Options) (*Instance, error) {
	return acquireInstance(id, opts, osFileOps())
}

func acquireInstance(id spec.Identity, opts paths.Options, ops fileOps) (*Instance, error) {
	dirs, err := paths.Resolve(id, opts, paths.Runtime)
	if err != nil {
		return nil, fmt.Errorf("daemon: instance: %w", err)
	}

	if err := dirs.Ensure(paths.Runtime); err != nil {
		return nil, fmt.Errorf("daemon: instance: %w", err)
	}

	dir := dirs.Runtime()
	lockPath := filepath.Join(dir, lockName)
	holderPath := filepath.Join(dir, holderName)

	unlock, err := filelock.New(lockPath).TryLock()
	if err != nil {
		if errors.Is(err, filelock.ErrLocked) {
			return nil, &AlreadyRunningError{Path: lockPath, PID: readHolder(holderPath), err: err}
		}

		return nil, fmt.Errorf("daemon: instance: %w", err)
	}

	in := &Instance{path: lockPath, holderPath: holderPath, ops: ops, unlock: unlock}
	in.writeHolder(filepath.Join(dir, holderTmpName))

	return in, nil
}

// Path returns the path of the lock file.
func (in *Instance) Path() string {
	return in.path
}

// Release removes the holder record and releases the lock. It is safe to call more than once and
// from several goroutines; only the first call acts.
func (in *Instance) Release() {
	in.once.Do(func() {
		// The record only describes the holder; a leftover names a process that no longer holds
		// the lock, and the next acquire replaces it.
		_ = in.ops.remove(in.holderPath)

		in.unlock()
	})
}

// writeHolder writes the pid through tmp and renames it over the record, ignoring failures.
func (in *Instance) writeHolder(tmp string) {
	data := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := in.ops.writeFile(tmp, data, holderPerm); err != nil {
		return
	}

	_ = in.ops.rename(tmp, in.holderPath)
}

// readHolder returns the pid in the record at path, and 0 when it cannot be read or parsed.
func readHolder(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}

	return pid
}
