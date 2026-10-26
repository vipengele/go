//go:build !windows

package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDownloadJoinsRemoveFailure runs only where an open file can be removed: on Windows the
// handler cannot remove the temporary file the download holds open.
func TestDownloadJoinsRemoveFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	wrong := strings.Repeat("x", len(dlContent))

	// The handler removes the temporary file itself once it exists, so the removal on the failure
	// path finds nothing to remove.
	archive := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, wrong[:5])
		w.(http.Flusher).Flush()
		deadline := time.Now().Add(5 * time.Second)
		for {
			entries, err := os.ReadDir(dir)
			if err == nil && len(entries) == 1 {
				_ = os.Remove(filepath.Join(dir, entries[0].Name()))
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		_, _ = io.WriteString(w, wrong[5:])
	}
	f := newFakeRelease(t, nil, archive)

	a, err := f.config(t, dir).Download(context.Background(), f.selection())
	if a != nil {
		t.Errorf("Download() Archive = %v, want nil", a)
	}
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Errorf("Download() error = %v, want a *ChecksumMismatchError", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Download() error = %v, want the removal's os.ErrNotExist joined", err)
	}
	assertDirEmpty(t, dir)
}
