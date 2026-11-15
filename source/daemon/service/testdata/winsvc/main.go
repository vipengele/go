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
	if path := os.Getenv("WINSVC_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	logger := slog.New(slog.NewTextHandler(out, nil)).With("pid", os.Getpid())

	marker := os.Getenv("WINSVC_MARKER")
	if marker == "" {
		return errors.New("WINSVC_MARKER is not set")
	}

	s, err := daemon.New([]daemon.Runner{daemon.Func("marker", func(ctx context.Context) error {
		return waitForMarker(ctx, logger, marker)
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

// waitForMarker returns ctx's error once ctx ends, or ErrRestartRequired once
// it has removed the marker file. A marker it cannot remove is logged and
// looked for again, so that one marker asks for one restart.
func waitForMarker(ctx context.Context, logger *slog.Logger, marker string) error {
	t := time.NewTicker(markerPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if _, err := os.Stat(marker); err != nil {
			continue
		}
		if err := os.Remove(marker); err != nil {
			logger.Warn("winsvc: remove the marker", "err", err)
			continue
		}
		logger.Info("winsvc: marker found, restart requested")
		return daemon.ErrRestartRequired
	}
}
