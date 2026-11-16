// Command winsvc is the service the end-to-end test of the Windows driver
// installs. It runs one Runner under daemon.WithWindowsService that waits until
// it is stopped or until the file named by WINSVC_MARKER appears; it then
// removes the file and asks for a restart with daemon.ErrRestartRequired. It
// logs to the file named by WINSVC_LOG, when set, which the test prints when it
// fails.
//
// The test builds it by its explicit path, go build ./testdata/winsvc, so no
// pattern of the module's packages lints, vets or covers it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.vipengele.org/daemon"
)

// markerPoll is the interval between two looks for the marker file.
const markerPoll = 200 * time.Millisecond

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "winsvc:", err)
		os.Exit(1)
	}
}

func run() error {
	var out io.Writer = io.Discard
	if logPath := os.Getenv("WINSVC_LOG"); logPath != "" {
		dir, name, err := openDir(logPath)
		if err != nil {
			return err
		}
		f, err := dir.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		_ = dir.Close()
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	logger := slog.New(slog.NewTextHandler(out, nil)).With("pid", os.Getpid())

	markerPath := os.Getenv("WINSVC_MARKER")
	if markerPath == "" {
		return errors.New("WINSVC_MARKER is not set")
	}
	markerDir, marker, err := openDir(markerPath)
	if err != nil {
		return err
	}
	defer func() { _ = markerDir.Close() }()

	s, err := daemon.New([]daemon.Runner{daemon.Func("marker", func(ctx context.Context) error {
		return waitForMarker(ctx, logger, markerDir, marker)
	})}, daemon.WithWindowsService(), daemon.WithGrace(5*time.Second), daemon.WithLogger(logger))
	if err != nil {
		return err
	}
	logger.Info("winsvc: run")
	err = s.Run(context.Background())
	logger.Info("winsvc: run returned", "err", err, "restart", daemon.RestartRequested(err))
	if err != nil && !daemon.RestartRequested(err) {
		return err
	}
	return nil
}

// openDir opens the directory of path as an os.Root and returns it with the
// last element of path, the name every access goes through, so that a path
// from the environment reaches no file outside its own directory.
func openDir(path string) (*os.Root, string, error) {
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	return dir, filepath.Base(path), nil
}

// waitForMarker returns ctx's error once ctx ends, or ErrRestartRequired once
// it has removed the marker file, the file marker in dir. A marker it cannot
// remove is logged and looked for again, so that one marker asks for one
// restart.
func waitForMarker(ctx context.Context, logger *slog.Logger, dir *os.Root, marker string) error {
	t := time.NewTicker(markerPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if _, err := dir.Stat(marker); err != nil {
			continue
		}
		if err := dir.Remove(marker); err != nil {
			logger.Warn("winsvc: remove the marker", "err", err)
			continue
		}
		logger.Info("winsvc: marker found, restart requested")
		return daemon.ErrRestartRequired
	}
}
