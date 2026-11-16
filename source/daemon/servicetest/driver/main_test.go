package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.vipengele.org/daemon/service"
)

// fakeManager records the operation it was asked to perform.
type fakeManager struct {
	service.Manager
	calls  []string
	err    error
	status service.Status
}

func (f *fakeManager) Install(context.Context) error {
	f.calls = append(f.calls, "install")
	return f.err
}

func (f *fakeManager) Uninstall(context.Context) error {
	f.calls = append(f.calls, "uninstall")
	return f.err
}

func (f *fakeManager) Status(context.Context) (service.Status, error) {
	f.calls = append(f.calls, "status")
	return f.status, f.err
}

func provide(m service.Manager, err error) newManager {
	return func(service.Config, ...service.Option) (service.Manager, error) { return m, err }
}

func runWith(t *testing.T, in string, mk newManager) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := run(context.Background(), strings.NewReader(in), &out, mk)
	return code, out.String()
}

func TestRunRejectsMalformedInput(t *testing.T) {
	code, out := runWith(t, "{not json", provide(&fakeManager{}, nil))
	if code != exitBadArgs {
		t.Fatalf("code = %d, want %d", code, exitBadArgs)
	}
	if !strings.Contains(out, `"error":"decode request:`) {
		t.Fatalf("out = %q", out)
	}
}

func TestRunRejectsUnknownOp(t *testing.T) {
	f := &fakeManager{}
	code, out := runWith(t, `{"Op":"restart"}`, provide(f, nil))
	if code != exitBadArgs {
		t.Fatalf("code = %d, want %d", code, exitBadArgs)
	}
	if want := `{"error":"unknown op \"restart\""}` + "\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls = %v, want none", f.calls)
	}
}

func TestRunPerformsTheOp(t *testing.T) {
	tests := []struct {
		op, want string
	}{
		{"install", "{}\n"},
		{"uninstall", "{}\n"},
		{"status", `{"status":{"state":"running","enabled":true,"pid":7,"restarts":0,"detail":"d"}}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			f := &fakeManager{status: service.Status{State: service.StateRunning, Enabled: true, PID: 7, Detail: "d"}}
			code, out := runWith(t, `{"Op":"`+tt.op+`"}`, provide(f, nil))
			if code != exitOK {
				t.Fatalf("code = %d, want 0; out = %q", code, out)
			}
			if out != tt.want {
				t.Fatalf("out = %q, want %q", out, tt.want)
			}
			if len(f.calls) != 1 || f.calls[0] != tt.op {
				t.Fatalf("calls = %v, want [%s]", f.calls, tt.op)
			}
		})
	}
}

func TestRunReportsAManagerError(t *testing.T) {
	f := &fakeManager{err: errors.New("boom")}
	code, out := runWith(t, `{"Op":"install"}`, provide(f, nil))
	if code != exitFailed {
		t.Fatalf("code = %d, want %d", code, exitFailed)
	}
	if want := `{"error":"boom"}` + "\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestRunReportsTheStatusWithItsError(t *testing.T) {
	f := &fakeManager{err: service.ErrManagerUnreachable, status: service.Status{State: service.StateUnknown, Detail: "no bus"}}
	code, out := runWith(t, `{"Op":"status"}`, provide(f, nil))
	if code != exitFailed {
		t.Fatalf("code = %d, want %d", code, exitFailed)
	}
	if !strings.Contains(out, `"state":"unknown"`) || !strings.Contains(out, `"error":"service: service manager unreachable"`) {
		t.Fatalf("out = %q", out)
	}
}

func TestRunReportsAConstructionError(t *testing.T) {
	code, out := runWith(t, `{"Op":"status"}`, provide(nil, service.ErrInvalidConfig))
	if code != exitFailed {
		t.Fatalf("code = %d, want %d", code, exitFailed)
	}
	if want := `{"error":"service: invalid config"}` + "\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestRunPassesTheRequestToTheConstructor(t *testing.T) {
	var got service.Config
	var nOpts int
	mk := func(c service.Config, opts ...service.Option) (service.Manager, error) {
		got, nOpts = c, len(opts)
		return &fakeManager{}, nil
	}
	code, out := runWith(t, `{"Op":"install","Config":{"Name":"svc"},"Notify":{"Watchdog":1000000000}}`, mk)
	if code != exitOK {
		t.Fatalf("code = %d; out = %q", code, out)
	}
	if got.Name != "svc" {
		t.Fatalf("Name = %q, want svc", got.Name)
	}
	if nOpts != 2 {
		t.Fatalf("options = %d, want 2 (container, notify)", nOpts)
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunReportsAFailedWrite(t *testing.T) {
	f := &fakeManager{}
	code := run(context.Background(), strings.NewReader(`{"Op":"install"}`), failingWriter{}, provide(f, nil))
	if code != exitFailed {
		t.Fatalf("code = %d, want %d", code, exitFailed)
	}
}

const asDriverEnv = "SERVICETEST_DRIVER_AS_MAIN"

// TestMain runs main itself when the test binary is re-executed as the driver.
func TestMain(m *testing.M) {
	if os.Getenv(asDriverEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func TestMainExitsWithTheRunCode(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^$"}
	if dir := flag.Lookup("test.gocoverdir"); dir != nil && dir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+dir.Value.String())
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), asDriverEnv+"=1")
	cmd.Stdin = strings.NewReader("{not json")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != exitBadArgs {
		t.Fatalf("err = %v, want exit code %d", err, exitBadArgs)
	}
	if !strings.Contains(out.String(), `"error":"decode request:`) {
		t.Fatalf("out = %q", out.String())
	}
}
