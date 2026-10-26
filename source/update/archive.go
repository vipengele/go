package update

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Archive is a downloaded release archive whose SHA-256 digest has been verified: an open
// temporary file, positioned at offset 0 when [Config.Download] returns it. It reads like an
// [os.File] but cannot be closed without being removed: [Archive.Close] does both.
//
// An Archive is not safe for concurrent use, except that [Archive.ReadAt] may be called
// concurrently with itself.
type Archive struct {
	f    *os.File
	size int64
	// remove deletes the file by name. createArchive sets it to [os.Remove].
	remove func(string) error
	closed bool
}

// createArchive creates an empty temporary file in dir, named prefix followed by a random suffix,
// with mode 0600. dir must exist: a missing directory is an error and is not created. The caller
// writes the archive through a.f, then calls finish, or discard on a failure.
func createArchive(dir, prefix string) (*Archive, error) {
	f, err := os.CreateTemp(dir, prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("update: creating temporary file: %w", err)
	}
	return &Archive{f: f, remove: os.Remove}, nil
}

// finish records the size of the file and rewinds it to offset 0, ready to be returned. On a
// failure the file is discarded and the error carries any failure to remove it.
func (a *Archive) finish() (*Archive, error) {
	info, err := a.f.Stat()
	if err == nil {
		_, err = a.f.Seek(0, io.SeekStart)
	}
	if err != nil {
		return nil, a.discard(fmt.Errorf("update: rewinding temporary file: %w", err))
	}
	a.size = info.Size()
	return a, nil
}

// discard closes and removes the file and returns err joined with any error doing so. It is the
// failure path of a download: no temporary file outlives the error it returns.
func (a *Archive) discard(err error) error {
	return errors.Join(err, a.Close())
}

// Read reads from the archive at its current offset, as [os.File.Read] does.
func (a *Archive) Read(p []byte) (int, error) {
	return a.f.Read(p)
}

// ReadAt reads len(p) bytes from the archive at offset off, as [os.File.ReadAt] does. It does not
// move the offset Read and Seek use.
func (a *Archive) ReadAt(p []byte, off int64) (int, error) {
	return a.f.ReadAt(p, off)
}

// Seek sets the offset of the next Read, as [os.File.Seek] does.
func (a *Archive) Seek(offset int64, whence int) (int64, error) {
	return a.f.Seek(offset, whence)
}

// Size is the archive's length in bytes: every byte that was written and verified, and the bound
// of what ReadAt reads.
func (a *Archive) Size() int64 {
	return a.size
}

// Name is the path of the temporary file. The file is removed by Close, so the name is valid only
// until then.
func (a *Archive) Name() string {
	return a.f.Name()
}

// Close closes the archive and removes its temporary file, returning both errors joined. A second
// call does nothing and returns nil.
//
// The file is closed before it is removed because Windows refuses to remove an open file. On Linux
// removing an open file succeeds, so no test run there can show the order matters.
func (a *Archive) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	closeErr := a.f.Close()
	removeErr := a.remove(a.f.Name())
	return errors.Join(
		wrapNonNil("update: closing temporary file", closeErr),
		wrapNonNil("update: removing temporary file", removeErr),
	)
}

// wrapNonNil prefixes err with msg, or returns nil when err is nil.
func wrapNonNil(msg string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
