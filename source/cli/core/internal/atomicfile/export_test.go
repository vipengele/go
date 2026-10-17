package atomicfile

// SyncDir exposes syncDir to the external tests.
func SyncDir(dir string) error { return syncDir(dir) }
