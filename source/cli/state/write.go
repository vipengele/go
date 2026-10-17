package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// steps is the write path, one field per filesystem operation, in the order WriteFile performs
// them: Create, Write, CloseWritten, Open, Chmod, Sync, CloseSynced, Rename, SyncDir. Replace
// starts at Open. Open names the temporary file by path, so Chmod, Sync and CloseSynced act on
// the inode the path names when Replace is called, never on the handle Create returned.
type steps struct {
	Create       func(dir, pattern string) (*os.File, error)
	Write        func(f *os.File, data []byte) (int, error)
	CloseWritten func(f *os.File) error
	Open         func(name string) (*os.File, error)
	Chmod        func(f *os.File, mode os.FileMode) error
	Sync         func(f *os.File) error
	CloseSynced  func(f *os.File) error
	Rename       func(oldpath, newpath string) error
	SyncDir      func(dir string) error
}

// newSteps returns the write path that operates on the real filesystem.
func newSteps() steps {
	return steps{
		Create:       os.CreateTemp,
		Write:        (*os.File).Write,
		CloseWritten: (*os.File).Close,
		Open:         func(name string) (*os.File, error) { return os.Open(filepath.Clean(name)) },
		Chmod:        (*os.File).Chmod,
		Sync:         (*os.File).Sync,
		CloseSynced:  (*os.File).Close,
		Rename:       os.Rename,
		SyncDir:      syncDir,
	}
}

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
	return newSteps().replace(tmp, final, mode)
}

// WriteFile writes data to path with the permission bits mode, atomically: it writes a
// temporary file in path's directory and moves it over path with Replace. The temporary file
// never remains after WriteFile returns, whether it succeeds or fails. The directory must
// exist.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	return newSteps().writeFile(path, data, mode)
}

func (s steps) writeFile(path string, data []byte, mode os.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	f, err := s.Create(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("state: create temp for %s: %w", path, err)
	}

	tmp := f.Name()

	_, writeErr := s.Write(f, data)
	if err := errors.Join(writeErr, s.CloseWritten(f)); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("state: write %s: %w", tmp, err)
	}

	return s.replace(tmp, path, mode)
}

func (s steps) replace(tmp, final string, mode os.FileMode) error {
	if err := s.prepare(tmp, mode); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	if err := s.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("state: rename %s to %s: %w", tmp, final, err)
	}

	return s.SyncDir(filepath.Dir(final))
}

// prepare opens tmp by path, sets its mode and syncs its content. Opening before chmod means a
// mode without read permission for the owner cannot prevent the sync.
func (s steps) prepare(tmp string, mode os.FileMode) error {
	f, err := s.Open(tmp)
	if err != nil {
		return fmt.Errorf("state: open %s: %w", tmp, err)
	}

	if err := s.Chmod(f, mode); err != nil {
		_ = f.Close()

		return fmt.Errorf("state: chmod %s: %w", tmp, err)
	}

	if err := s.Sync(f); err != nil {
		_ = f.Close()

		return fmt.Errorf("state: sync %s: %w", tmp, err)
	}

	if err := s.CloseSynced(f); err != nil {
		return fmt.Errorf("state: close %s: %w", tmp, err)
	}

	return nil
}

// syncDir makes a rename into dir durable. Some filesystems cannot sync a directory and answer
// EINVAL or ENOTSUP; on those the rename is as durable as the filesystem allows and the file is
// already in place, so reporting the replace as failed would be wrong and the error is ignored.
// Any other error is returned.
func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("state: open directory %s: %w", dir, err)
	}

	syncErr := d.Sync()
	closeErr := d.Close()

	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) && !errors.Is(syncErr, syscall.ENOTSUP) {
		return fmt.Errorf("state: sync directory %s: %w", dir, syncErr)
	}

	if closeErr != nil {
		return fmt.Errorf("state: close directory %s: %w", dir, closeErr)
	}

	return nil
}
