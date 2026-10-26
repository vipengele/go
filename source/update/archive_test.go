package update

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const archiveContent = "archive bytes"

// newFinishedArchive returns an Archive in a fresh temp dir holding archiveContent, ready to be
// returned by a download, along with that dir. The Archive is closed when the test ends, which
// does nothing for a test that has closed it already.
func newFinishedArchive(t *testing.T) (*Archive, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := createArchive(dir, "tool-update-")
	if err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if _, err := a.f.WriteString(archiveContent); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := a.finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got != a {
		t.Fatalf("finish returned %p, want %p", got, a)
	}
	return a, dir
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s holds %v, want it empty", dir, names)
	}
}

func TestCreateArchiveNameAndMode(t *testing.T) {
	t.Parallel()

	a, dir := newFinishedArchive(t)

	if got := filepath.Dir(a.Name()); got != dir {
		t.Errorf("Name dir = %q, want %q", got, dir)
	}
	if base := filepath.Base(a.Name()); !strings.HasPrefix(base, "tool-update-") || base == "tool-update-" {
		t.Errorf("Name base = %q, want the prefix and a random suffix", base)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(a.Name())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want 0600", got)
	}
}

func TestCreateArchiveMissingDir(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	missing := filepath.Join(parent, "missing")

	a, err := createArchive(missing, "tool-update-")
	if err == nil {
		t.Fatalf("createArchive(%q) = %v, nil; want an error", missing, a)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want it to wrap os.ErrNotExist", err)
	}
	if a != nil {
		t.Errorf("Archive = %v, want nil", a)
	}
	assertDirEmpty(t, parent)
}

func TestArchiveRewoundOnFinish(t *testing.T) {
	t.Parallel()

	a, _ := newFinishedArchive(t)

	off, err := a.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if off != 0 {
		t.Errorf("offset = %d, want 0", off)
	}
	got, err := io.ReadAll(a)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != archiveContent {
		t.Errorf("Read = %q, want %q", got, archiveContent)
	}
}

func TestArchiveReadAtAndSizeAgree(t *testing.T) {
	t.Parallel()

	a, _ := newFinishedArchive(t)

	if got, want := a.Size(), int64(len(archiveContent)); got != want {
		t.Fatalf("Size = %d, want %d", got, want)
	}
	buf := make([]byte, a.Size())
	n, err := a.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if int64(n) != a.Size() || string(buf) != archiveContent {
		t.Errorf("ReadAt = %d, %q; want %d, %q", n, buf, a.Size(), archiveContent)
	}
	if n, err := a.ReadAt(make([]byte, 1), a.Size()); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("ReadAt at Size = %d, %v; want 0, io.EOF", n, err)
	}
}

func TestArchiveCloseRemovesFile(t *testing.T) {
	t.Parallel()

	a, dir := newFinishedArchive(t)

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertDirEmpty(t, dir)
	if _, err := a.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Read after Close = %v, want os.ErrClosed", err)
	}
}

func TestArchiveCloseTwice(t *testing.T) {
	t.Parallel()

	a, dir := newFinishedArchive(t)

	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	assertDirEmpty(t, dir)
}

func TestArchiveCloseRemoveError(t *testing.T) {
	t.Parallel()

	errRemove := errors.New("remove failed")

	a, dir := newFinishedArchive(t)
	a.remove = func(name string) error {
		if err := os.Remove(name); err != nil {
			t.Errorf("os.Remove: %v", err)
		}
		return errRemove
	}

	err := a.Close()
	if !errors.Is(err, errRemove) {
		t.Errorf("Close = %v, want it to wrap the remove error", err)
	}
	if errors.Is(err, os.ErrClosed) {
		t.Errorf("Close = %v, want no close error", err)
	}
	assertDirEmpty(t, dir)
}

func TestArchiveCloseJoinsCloseAndRemoveErrors(t *testing.T) {
	t.Parallel()

	errRemove := errors.New("remove failed")

	a, _ := newFinishedArchive(t)
	if err := a.f.Close(); err != nil {
		t.Fatalf("closing the file: %v", err)
	}
	var removed string
	a.remove = func(name string) error {
		removed = name
		return errors.Join(os.Remove(name), errRemove)
	}

	err := a.Close()
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("Close = %v, want it to wrap the close error", err)
	}
	if !errors.Is(err, errRemove) {
		t.Errorf("Close = %v, want it to wrap the remove error", err)
	}
	if removed != a.Name() {
		t.Errorf("removed %q, want %q", removed, a.Name())
	}
}

func TestArchiveDiscard(t *testing.T) {
	t.Parallel()

	errCause := errors.New("download failed")

	dir := t.TempDir()
	a, err := createArchive(dir, "tool-update-")
	if err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	if _, err := a.f.WriteString(archiveContent); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = a.discard(errCause)
	if err == nil || err.Error() != errCause.Error() {
		t.Errorf("discard = %v, want exactly %v", err, errCause)
	}
	if !errors.Is(err, errCause) {
		t.Errorf("discard = %v, want it to wrap the cause", err)
	}
	assertDirEmpty(t, dir)
}

func TestArchiveDiscardJoinsRemoveError(t *testing.T) {
	t.Parallel()

	errCause := errors.New("download failed")
	errRemove := errors.New("remove failed")

	dir := t.TempDir()
	a, err := createArchive(dir, "tool-update-")
	if err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	a.remove = func(name string) error {
		return errors.Join(os.Remove(name), errRemove)
	}

	err = a.discard(errCause)
	if !errors.Is(err, errCause) || !errors.Is(err, errRemove) {
		t.Errorf("discard = %v, want it to wrap the cause and the remove error", err)
	}
	assertDirEmpty(t, dir)
}

func TestArchiveFinishFailureDiscards(t *testing.T) {
	t.Parallel()

	errRemove := errors.New("remove failed")

	dir := t.TempDir()
	a, err := createArchive(dir, "tool-update-")
	if err != nil {
		t.Fatalf("createArchive: %v", err)
	}
	if err := a.f.Close(); err != nil {
		t.Fatalf("closing the file: %v", err)
	}
	a.remove = func(name string) error {
		return errors.Join(os.Remove(name), errRemove)
	}

	got, err := a.finish()
	if got != nil {
		t.Errorf("finish Archive = %v, want nil", got)
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("finish = %v, want it to wrap the failure", err)
	}
	if !errors.Is(err, errRemove) {
		t.Errorf("finish = %v, want it to wrap the remove error", err)
	}
	if !strings.Contains(err.Error(), "rewinding temporary file") {
		t.Errorf("finish = %v, want it to name the rewind", err)
	}
	assertDirEmpty(t, dir)
}
