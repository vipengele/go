//go:build unix

package update

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The tests in this file run a real staged script and never call t.Parallel: a process another
// test forks inherits every descriptor open at that moment until it execs, so a staged file some
// parallel test still holds open for writing fails that test's exec with ETXTBSY, and the script
// run here holds open whatever descriptors it inherited the same way.

// stageScript writes script to a staged file with its write descriptor open, as Extract leaves
// it, and makes it runnable.
func stageScript(t *testing.T, script string) *Staged {
	t.Helper()

	s := newStagedFile(t)
	if _, err := s.f.WriteString(script); err != nil {
		t.Fatalf("writing the script: %v", err)
	}

	if err := s.f.Chmod(stagedMode); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	return s
}

// hostConfig is a valid Config for the platform the test runs on.
func hostConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.GOOS = runtime.GOOS
	c.GOARCH = runtime.GOARCH

	return c
}

func TestCheckStagedRunsTheStagedBinary(t *testing.T) {
	script := "#!/bin/sh\n" +
		"[ \"$1 $2\" = \"version --json\" ] || exit 9\n" +
		"cat <<'EOF'\n" + versionJSON("v1.2.3", runtime.GOOS, runtime.GOARCH) + "EOF\n"
	s := stageScript(t, script)

	got, err := hostConfig(t).CheckStaged(context.Background(), s, Selection{Tag: preflightTag})
	if err != nil {
		t.Fatalf("CheckStaged: %v", err)
	}

	want := StagedBinary{Path: s.Name(), Version: "v1.2.3", Schema: 1, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if got != want {
		t.Errorf("StagedBinary = %+v, want %+v", got, want)
	}
}

func TestCheckStagedReadsNothingFromStdin(t *testing.T) {
	script := "#!/bin/sh\n" +
		"if read -r line; then exit 7; fi\n" +
		"cat <<'EOF'\n" + versionJSON("v1.2.3", runtime.GOOS, runtime.GOARCH) + "EOF\n"

	if _, err := hostConfig(t).CheckStaged(context.Background(), stageScript(t, script), Selection{Tag: preflightTag}); err != nil {
		t.Errorf("CheckStaged: %v, want stdin at end of file", err)
	}
}

func TestCheckStagedRefusesANonZeroExit(t *testing.T) {
	script := "#!/bin/sh\n" +
		"printf '%03000d' 0 >&2\n" +
		"exit 3\n"
	s := stageScript(t, script)

	_, err := hostConfig(t).CheckStaged(context.Background(), s, Selection{Tag: preflightTag})

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("CheckStaged error = %v, want a *PreflightError", err)
	}

	if want := strings.Repeat("0", maxErrorBody); pe.Stderr != want {
		t.Errorf("Stderr is %d bytes, want %d", len(pe.Stderr), len(want))
	}

	if pe.Err == nil || !strings.Contains(pe.Err.Error(), "exit status 3") {
		t.Errorf("Err = %v, want the exit status", pe.Err)
	}
}

func TestCheckStagedRefusesOutputOver64KiB(t *testing.T) {
	script := "#!/bin/sh\n" +
		"while :; do printf '%01024d\\n' 0; done\n"

	c := hostConfig(t)
	c.PreflightTimeout = time.Minute

	_, err := c.CheckStaged(context.Background(), stageScript(t, script), Selection{Tag: preflightTag})

	var pe *PreflightError
	if !errors.As(err, &pe) || !strings.Contains(pe.Reason, "more than") {
		t.Fatalf("CheckStaged error = %v, want a *PreflightError for the output limit", err)
	}
}

func TestCheckStagedKillsABinaryPastPreflightTimeout(t *testing.T) {
	s := stageScript(t, "#!/bin/sh\nexec sleep 30\n")

	c := hostConfig(t)
	c.PreflightTimeout = 200 * time.Millisecond

	start := time.Now()
	_, err := c.CheckStaged(context.Background(), s, Selection{Tag: preflightTag})

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("CheckStaged took %v, want the binary killed at the timeout", elapsed)
	}

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("CheckStaged error = %v, want a *PreflightError", err)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CheckStaged error = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

func TestCheckStagedPassesAParentCancelThroughARun(t *testing.T) {
	s := stageScript(t, "#!/bin/sh\nexec sleep 30\n")

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(200*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()

	_, err := hostConfig(t).CheckStaged(ctx, s, Selection{Tag: preflightTag})
	assertParentCancel(t, err)

	if _, statErr := os.Stat(s.Name()); statErr != nil {
		t.Errorf("Stat(staged) = %v, want the staged file left to the caller", statErr)
	}
}
