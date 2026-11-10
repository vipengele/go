//go:build unix

package update

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestExtractMode runs only where files carry Unix permission bits, which Windows lacks.
func TestExtractMode(t *testing.T) {
	t.Parallel()

	a, _ := newExtractArchive(t, []byte("bin"))

	s, err := rawConfig(t).Extract(context.Background(), a, t.TempDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	keepAtCleanup(t, s)

	info, err := os.Stat(s.Name())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %v, want 0700", got)
	}
}

// TestStagedRunsAfterCloseFile runs a script staged by Extract once closeFile has closed its write
// descriptor: a file still open for writing fails to execve with ETXTBSY. It does not run in
// parallel, because a process another test forks inherits every descriptor open at that moment
// until it execs, and holding the staged file open for writing there fails the exec all the same.
func TestStagedRunsAfterCloseFile(t *testing.T) {
	script := "#!/bin/sh\necho staged ran\n"
	a, _ := newExtractArchive(t, tarGzOf(t, tarFile("atk", script)))

	s, err := tarGzConfig(t).Extract(context.Background(), a, t.TempDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	t.Cleanup(func() {
		if err := s.Discard(); err != nil {
			t.Errorf("Discard: %v", err)
		}
	})

	if err := s.closeFile(); err != nil {
		t.Fatalf("closeFile: %v", err)
	}

	out, err := exec.CommandContext(context.Background(), s.Name()).Output()
	if err != nil {
		t.Fatalf("running the staged binary: %v", err)
	}

	if got, want := string(out), "staged ran\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
