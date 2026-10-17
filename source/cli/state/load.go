package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// ErrCorrupt is matched, with errors.Is, by every error LoadJSON and LoadYAML return with the
// status Corrupt: the file was read but its content does not decode. The error also wraps the
// decoder's own error.
var ErrCorrupt = errors.New("state: corrupt content")

// LoadStatus is what a load found at a path.
type LoadStatus int

const (
	// Missing means no file exists at the path. It comes with a nil error.
	Missing LoadStatus = iota
	// Loaded means the file was read and decoded.
	Loaded
	// Corrupt means the file was read and its content does not decode, an empty file included.
	// The error matches ErrCorrupt.
	Corrupt
	// Unreadable means the file exists, or might, and could not be read: permission was denied,
	// the path names a directory, or the read failed. The error wraps the filesystem's error and
	// does not match ErrCorrupt.
	Unreadable
)

// String returns "missing", "loaded", "corrupt" or "unreadable", and "LoadStatus(n)" for any other
// value.
func (s LoadStatus) String() string {
	switch s {
	case Missing:
		return "missing"
	case Loaded:
		return "loaded"
	case Corrupt:
		return "corrupt"
	case Unreadable:
		return "unreadable"
	default:
		return "LoadStatus(" + strconv.Itoa(int(s)) + ")"
	}
}

// load reads path and decodes it with decode. On any status but Loaded it returns the zero T, so
// a value decode filled in part before failing never reaches the caller.
func load[T any](path string, decode func(data []byte, v *T) error) (T, LoadStatus, error) {
	var zero T

	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return zero, Missing, nil
	}

	if err != nil {
		return zero, Unreadable, fmt.Errorf("state: read %s: %w", path, err)
	}

	if len(data) == 0 {
		return zero, Corrupt, fmt.Errorf("%w: %s: the file is empty", ErrCorrupt, path)
	}

	var v T
	if err := decode(data, &v); err != nil {
		return zero, Corrupt, fmt.Errorf("%w: %s: %w", ErrCorrupt, path, err)
	}

	return v, Loaded, nil
}
