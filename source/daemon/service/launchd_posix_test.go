// The tests in this file rest on POSIX hosts: the permission bits of a file
// or directory, POSIX absolute paths, ENOTDIR, which Windows reports as a
// missing path, for a path through a regular file, and the id command.

//go:build !windows

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdInstallPlacesThePlistByScope(t *testing.T) {
	agent := newTestLaunchd(t, ScopeUser, 501)
	if got, want := agent.plist(t), filepath.Join(agent.homeDir, "Library", "LaunchAgents", "demo.plist"); got != want {
		t.Fatalf("agent plist = %q, want %q", got, want)
	}
	daemon := newTestLaunchd(t, ScopeSystem, 0)
	daemon.daemonDir = ""
	if got, want := daemon.plist(t), "/Library/LaunchDaemons/demo.plist"; got != want {
		t.Fatalf("daemon plist = %q, want %q", got, want)
	}
}

func TestLaunchdInstallWritesTheRenderedPlist(t *testing.T) {
	for _, scope := range []Scope{ScopeSystem, ScopeUser} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			tl := newTestLaunchd(t, scope, 0)
			tl.runner.on("launchctl print "+tl.target(), notFound())
			if err := tl.Install(context.Background()); err != nil {
				t.Fatalf("Install: %v", err)
			}
			path := tl.plist(t)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := tl.paths()
			if err != nil {
				t.Fatal(err)
			}
			if want := renderPlist(tl.cfg, p.logs); !bytes.Equal(got, want) {
				t.Fatalf("plist =\n%s\nwant\n%s", got, want)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o644 {
				t.Fatalf("plist mode = %v, want 0644", fi.Mode().Perm())
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("plist directory holds %d entries, want only the plist", len(entries))
			}
		})
	}
}

func TestLaunchdLogPathsTakeTheOptionOverTheDefault(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	tl.logs = launchdLogs{stderr: "/var/log/demo.err"}
	p, err := tl.paths()
	if err != nil {
		t.Fatal(err)
	}
	got := p.logs
	want := launchdLogs{
		stdout: filepath.Join(tl.homeDir, "Library", "Logs", "demo", "demo.log"),
		stderr: "/var/log/demo.err",
	}
	if got != want {
		t.Fatalf("logs = %+v, want %+v", got, want)
	}
}

func TestLaunchdPrepareCreatesThePlistAndLogDirectories(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	out := filepath.Join(t.TempDir(), "out", "demo.log")
	tl.logs = launchdLogs{stdout: out}
	if err := tl.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, dir := range []string{
		filepath.Dir(tl.plist(t)),
		filepath.Dir(out),
		filepath.Join(tl.homeDir, "Library", "Logs", "demo"),
	} {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			t.Fatalf("stat %s = %v, want a directory", dir, err)
		}
		if fi.Mode().Perm() != 0o750 {
			t.Fatalf("%s mode = %v, want 0750", dir, fi.Mode().Perm())
		}
	}
	if len(tl.runner.calls) != 0 {
		t.Fatalf("commands = %q, want none", tl.runner.calls)
	}
}

func TestLaunchdUnreadablePlistDirIsNotNotInstalled(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	blocker := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tl.homeDir = blocker
	err := tl.Start(context.Background())
	if err == nil || errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "stat plist") {
		t.Fatalf("err = %v, want a stat failure other than ErrNotInstalled", err)
	}
}

func TestExitCodeReadsTheRunnersError(t *testing.T) {
	if code, ok := exitCode(fmt.Errorf("launchctl print: %w", notFound())); !ok || code != launchctlNotFound {
		t.Fatalf("exitCode = %d, %v, want %d, true", code, ok, launchctlNotFound)
	}
	if code, ok := exitCode(errors.New("boom")); ok || code != 0 {
		t.Fatalf("exitCode = %d, %v, want 0, false", code, ok)
	}
	_, err := execRunner(context.Background(), []string{"id", "-u", "--", "no-such-account-vipengele-test"})
	if code, ok := exitCode(err); !ok || code == 0 {
		t.Fatalf("exitCode(%v) = %d, %v, want a non-zero exit status", err, code, ok)
	}
}

func TestLaunchdStatusPlistStatFailureIsUnknown(t *testing.T) {
	tl := newTestLaunchd(t, ScopeUser, 501)
	blocker := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tl.homeDir = blocker
	st, err := tl.Status(context.Background())
	if err == nil || errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "stat plist") {
		t.Fatalf("Status error = %v, want a stat failure other than ErrNotInstalled", err)
	}
	if st != (Status{State: StateUnknown}) {
		t.Fatalf("Status = %+v, want only StateUnknown", st)
	}
	if len(tl.runner.calls) != 0 {
		t.Fatalf("commands = %q, want none", tl.runner.calls)
	}
}
