package daemon

import (
	"context"
	"log/slog"
	"testing"
)

func TestWithLoggerRejectsNil(t *testing.T) {
	if _, err := New([]Runner{&fakeRunner{"a"}}, WithLogger(nil)); err == nil {
		t.Fatal("New accepted a nil logger")
	}
}

func TestRunUsesTheConfiguredLogger(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	s, err := New([]Runner{&fakeRunner{"a"}}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.Run(ctx)
	if s.log != logger {
		t.Fatalf("Run resolved logger %p, want the configured %p", s.log, logger)
	}
}

func TestRunResolvesTheDefaultLoggerWhenItBegins(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{"a"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	installed := slog.New(slog.DiscardHandler)
	slog.SetDefault(installed)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.Run(ctx)
	if s.log != installed {
		t.Fatalf("Run resolved logger %p, want the default installed after New %p", s.log, installed)
	}
}
