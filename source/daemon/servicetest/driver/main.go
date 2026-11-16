// Command driver is the program the servicetest harness runs inside the
// container. It reads one wire.Request as JSON from standard input, performs
// its operation on the service through service.New, and writes one JSON
// object to standard output.
//
// The object carries "status", the service.Status as the Manager reports it
// (its "state" is the lower-case State name, such as "running"), and "error",
// the message of the failure. A successful status operation writes
// {"status": {...}}, a successful install or uninstall writes {}, and a
// failure writes {"error": "..."}; a failed status operation also carries the
// "status" the Manager returned with its error.
//
// The exit code is 0 on success, 1 when the Manager could not be built or the
// operation failed, and 2 when the request could not be read or names an
// unknown operation.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"go.vipengele.org/daemon/service"
	"go.vipengele.org/daemon/servicetest/internal/wire"
)

const (
	exitOK      = 0
	exitFailed  = 1
	exitBadArgs = 2
)

// newManager builds the Manager that run drives.
type newManager func(service.Config, ...service.Option) (service.Manager, error)

func main() {
	os.Exit(run(context.Background(), os.Stdin, os.Stdout, service.New))
}

// run performs the request read from stdin through the Manager that mk
// returns, writes the response to stdout and returns the exit code.
func run(ctx context.Context, stdin io.Reader, stdout io.Writer, mk newManager) int {
	var req wire.Request
	dec := json.NewDecoder(stdin)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fail(stdout, exitBadArgs, nil, fmt.Errorf("decode request: %w", err))
	}
	switch req.Op {
	case wire.OpInstall, wire.OpUninstall, wire.OpStatus:
	default:
		return fail(stdout, exitBadArgs, nil, fmt.Errorf("unknown op %q", req.Op))
	}
	m, err := mk(req.Config, req.Options()...)
	if err != nil {
		return fail(stdout, exitFailed, nil, err)
	}
	switch req.Op {
	case wire.OpInstall:
		err = m.Install(ctx)
	case wire.OpUninstall:
		err = m.Uninstall(ctx)
	case wire.OpStatus:
		var st service.Status
		st, err = m.Status(ctx)
		if err != nil {
			return fail(stdout, exitFailed, &st, err)
		}
		return write(stdout, wire.Response{Status: &st}, exitOK)
	}
	if err != nil {
		return fail(stdout, exitFailed, nil, err)
	}
	return write(stdout, wire.Response{}, exitOK)
}

// fail writes err as the response and returns code.
func fail(stdout io.Writer, code int, st *service.Status, err error) int {
	return write(stdout, wire.Response{Status: st, Error: err.Error()}, code)
}

// write encodes r to stdout and returns code, or exitFailed when the write
// fails.
func write(stdout io.Writer, r wire.Response, code int) int {
	if err := json.NewEncoder(stdout).Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, "driver: write response:", err)
		return exitFailed
	}
	return code
}
