package filelock

import "os"

// Option exposes option to the external tests.
type Option = option

// PollInterval exposes pollInterval to the external tests.
const PollInterval = pollInterval

// WithLockCalls exposes withLockCalls, so a test can drive Lock's shared loop with fake system
// calls.
func WithLockCalls(tryLock func(f *os.File) (bool, error), unlockFile func(f *os.File) error) Option {
	return withLockCalls(tryLock, unlockFile)
}

// NewWith returns New's Locker on path, configured by opts.
func NewWith(path string, opts ...Option) Locker {
	return newLocker(path, opts...)
}
