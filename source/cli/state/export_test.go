package state

import "os"

// Steps exposes the write path to the external tests, so a test can replace one step.
type Steps = steps

// NewSteps exposes newSteps to the external tests.
func NewSteps() Steps { return newSteps() }

// WriteFileWith runs WriteFile through s.
func WriteFileWith(s Steps, path string, data []byte, mode os.FileMode) error {
	return s.writeFile(path, data, mode)
}

// SyncDir exposes syncDir to the external tests.
func SyncDir(dir string) error { return syncDir(dir) }
