// Package atomicfile replaces files so that a reader sees either the old content or the new,
// never a partial write, and so that the new content survives a crash once a call returns.
//
// A file is written to a temporary file in the destination's directory, given its final mode,
// synced, renamed over the destination, and the directory is synced so the rename itself is
// durable. Rename is atomic only within one filesystem, which is why the temporary file lives
// beside the destination.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Replace moves tmp over final with the permission bits mode.
//
// It is called after every write to tmp has finished. The mode is applied then rather than when
// tmp is created because a writer may have replaced tmp with a new inode in the meantime: editors
// that save by writing a new file and renaming it over the old one (vim with backupcopy=no,
// VS Code) leave a file at the umask default. Replace therefore opens tmp by path when it is
// called and chmods and syncs that inode, so whatever tmp names at that moment is what lands at
// final, at mode.
//
// tmp and final must be in the same directory, or at least on the same filesystem; a rename
// across filesystems fails and that failure is returned.
//
// On any failure before the rename, tmp is removed (best effort) and the error returned. Once
// the rename has succeeded tmp no longer exists, so a failure to sync the directory leaves
// final in place with the new content and is returned without removing anything.
func Replace(tmp, final string, mode os.FileMode) error {
	if err := prepare(tmp, mode); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("atomicfile: rename %s to %s: %w", tmp, final, err)
	}

	return syncDir(filepath.Dir(final))
}

// WriteFile writes data to path with the permission bits mode, atomically: it writes a
// temporary file in path's directory and moves it over path with Replace. The temporary file
// never remains after WriteFile returns, whether it succeeds or fails.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	f, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("atomicfile: create temp for %s: %w", path, err)
	}

	tmp := f.Name()

	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("atomicfile: write %s: %w", tmp, err)
	}

	return Replace(tmp, path, mode)
}

// prepare opens tmp by path, sets its mode and syncs its content. Opening before chmod means a
// mode without read permission for the owner cannot prevent the sync.
func prepare(tmp string, mode os.FileMode) error {
	f, err := os.Open(filepath.Clean(tmp))
	if err != nil {
		return fmt.Errorf("atomicfile: open %s: %w", tmp, err)
	}

	if err := f.Chmod(mode); err != nil {
		_ = f.Close()

		return fmt.Errorf("atomicfile: chmod %s: %w", tmp, err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()

		return fmt.Errorf("atomicfile: sync %s: %w", tmp, err)
	}

	return f.Close()
}

// syncDir makes a rename into dir durable. Some filesystems cannot sync a directory and answer
// EINVAL or ENOTSUP; on those the rename is as durable as the filesystem allows and the file is
// already in place, so reporting the replace as failed would be wrong and the error is ignored.
// Any other error is returned.
func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("atomicfile: open directory %s: %w", dir, err)
	}

	syncErr := d.Sync()
	closeErr := d.Close()

	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) && !errors.Is(syncErr, syscall.ENOTSUP) {
		return fmt.Errorf("atomicfile: sync directory %s: %w", dir, syncErr)
	}

	return closeErr
}
