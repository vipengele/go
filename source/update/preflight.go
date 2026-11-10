package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// maxPreflightOutput is the most bytes the staged binary may print on its standard output in
// answer to "version --json"; more is a refusal.
const maxPreflightOutput = 64 << 10

// preflightWaitDelay is how long the run of the staged binary waits, once the binary has exited
// or been killed, for its standard output and error to close. A child the binary started that
// keeps them open would otherwise hold the run past PreflightTimeout.
const preflightWaitDelay = time.Second

// errPreflightOutputLimit stops the copy of a standard output that exceeds [maxPreflightOutput].
var errPreflightOutputLimit = errors.New("update: version --json output exceeds the limit")

// Preflight is a tool's own check of a staged binary, run by [Config.CheckStaged] once the binary
// has answered "version --json" as the selected release for the platform. Returning an error vetoes
// the install. A hook may run b.Path; it is side-effect-free or cleans up after itself, because the
// binary may still be refused after it passes.
type Preflight func(ctx context.Context, b StagedBinary) error

// StagedBinary is a staged binary as its "version --json" answer describes it.
type StagedBinary struct {
	// Path is the path of the staged binary, [Staged.Name].
	Path string
	// Version is the version the binary reported, as it reported it: it may lack the leading "v"
	// of the selected tag.
	Version string
	// Schema is the schema of the binary's answer, 1 or greater.
	Schema int
	// OS is the operating system the binary reported it was built for.
	OS string
	// Arch is the architecture the binary reported it was built for.
	Arch string
}

// versionReport is the part of the "version --json" answer CheckStaged reads. Version is decoded
// on its own, so that a version that is not a string is refused for what it is rather than as
// undecodable output.
type versionReport struct {
	Schema  int             `json:"schema"`
	Version json.RawMessage `json:"version"`
	OS      string          `json:"os"`
	Arch    string          `json:"arch"`
}

// CheckStaged runs the staged binary s with "version --json" and refuses it unless it answers as
// the release sel selects, for the Config's platform; then it runs each [Config.Preflight] hook in
// order. It returns the binary as its answer describes it.
//
// The Config is resolved first, so an invalid one fails before anything runs. s must not be nil.
// CheckStaged closes the staged file's write descriptor, since a file open for writing cannot be
// run, but neither keeps nor discards s: the caller still settles it, discarding it on a refusal.
//
// The binary runs with the process's environment and with its standard input the null device, and
// is killed once PreflightTimeout expires. The answer is accepted when its standard output, at
// most 64 KiB, is one JSON object whose "schema" is 1 or greater and whose "version" is a
// non-empty string. The version, with a leading "v" added when it is missing, must equal sel.Tag
// exactly, and "os" and "arch" must equal the resolved GOOS and GOARCH. Unknown keys are ignored,
// so a binary with a newer schema is accepted.
//
// A refusal is a [*PreflightError], whether the binary failed to run, exited non-zero, timed out
// (its cause is then [context.DeadlineExceeded]), answered otherwise, or a hook returned an error
// (its cause is then that error). When ctx is done, its error is returned as it is instead. The
// StagedBinary is the zero value whenever the error is not nil.
func (c Config) CheckStaged(ctx context.Context, s *Staged, sel Selection) (StagedBinary, error) {
	return c.checkStaged(ctx, s, sel, defaultSteps())
}

// checkStaged is [Config.CheckStaged] through steps.
func (c Config) checkStaged(ctx context.Context, s *Staged, sel Selection, steps fsSteps) (StagedBinary, error) {
	r, err := c.resolve()
	if err != nil {
		return StagedBinary{}, err
	}

	if s == nil {
		return StagedBinary{}, errNoStaged
	}

	if err := s.closeFile(); err != nil {
		return StagedBinary{}, err
	}

	if err := ctx.Err(); err != nil {
		return StagedBinary{}, err
	}

	b, err := r.runPreflight(ctx, s.Name(), sel.Tag, steps)
	if err != nil {
		return StagedBinary{}, err
	}

	for i, hook := range r.Preflight {
		if hook == nil {
			continue
		}

		if err := hook(ctx, b); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return StagedBinary{}, ctxErr
			}

			return StagedBinary{}, &PreflightError{
				Path:   b.Path,
				Reason: "vetoed by Preflight[" + strconv.Itoa(i) + "]",
				Err:    err,
			}
		}
	}

	return b, nil
}

// runPreflight runs the binary at path with "version --json" through steps and checks its answer
// against tag and the resolved Config's platform: the built-in check of [Config.CheckStaged].
func (c Config) runPreflight(ctx context.Context, path, tag string, steps fsSteps) (StagedBinary, error) {
	// An absolute path keeps a bare file name from being looked up on PATH.
	exe, err := steps.abs(path)
	if err != nil {
		return StagedBinary{}, fmt.Errorf("update: resolving the staged binary %q: %w", path, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, c.PreflightTimeout)
	defer cancel()

	stdout := &capWriter{limit: maxPreflightOutput, overflow: errPreflightOutputLimit}
	stderr := &capWriter{limit: maxErrorBody + 1}
	runErr := steps.run(runCtx, exe, []string{"version", "--json"}, stdout, stderr)

	if err := ctx.Err(); err != nil {
		return StagedBinary{}, err
	}

	refuse := func(reason string, cause error) error {
		return &PreflightError{Path: path, Reason: reason, Stderr: cutErrorBody(string(stderr.buf)), Err: cause}
	}

	switch {
	case runErr != nil && runCtx.Err() != nil:
		return StagedBinary{}, refuse("version --json did not finish within "+c.PreflightTimeout.String(), runCtx.Err())
	case stdout.exceeded:
		return StagedBinary{}, refuse("version --json printed more than "+strconv.Itoa(maxPreflightOutput)+" bytes", nil)
	case runErr != nil:
		return StagedBinary{}, refuse("running version --json", runErr)
	}

	var rep versionReport
	if err := json.Unmarshal(stdout.buf, &rep); err != nil {
		return StagedBinary{}, refuse("version --json printed no JSON object", err)
	}

	if rep.Schema < 1 {
		return StagedBinary{}, refuse("version --json reported schema "+strconv.Itoa(rep.Schema)+", want 1 or greater", nil)
	}

	var reported string
	if err := json.Unmarshal(rep.Version, &reported); err != nil || reported == "" {
		return StagedBinary{}, refuse("version --json reported no version string", err)
	}

	b := StagedBinary{Path: path, Version: reported, Schema: rep.Schema, OS: rep.OS, Arch: rep.Arch}

	canonical := reported
	if !strings.HasPrefix(canonical, "v") {
		canonical = "v" + canonical
	}

	switch {
	case canonical != tag:
		return StagedBinary{}, refuse(fmt.Sprintf("reported version %q, want %q", reported, tag), nil)
	case rep.OS != c.GOOS:
		return StagedBinary{}, refuse(fmt.Sprintf("reported os %q, want %q", rep.OS, c.GOOS), nil)
	case rep.Arch != c.GOARCH:
		return StagedBinary{}, refuse(fmt.Sprintf("reported arch %q, want %q", rep.Arch, c.GOARCH), nil)
	}

	return b, nil
}

// runCommand runs the program name with args until it exits or ctx is done, when it is killed. Its
// environment is the process's own and its standard input the null device; its standard output
// and error are copied to stdout and stderr. It returns the error [exec.Cmd.Run] returns.
func runCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- name is the staged binary the updater extracted from a release archive whose checksum and signature it verified, into a file it created; it runs without a shell, with the fixed arguments version --json; running it is the purpose of CheckStaged
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name is the staged binary the updater extracted from a release archive whose checksum and signature it verified, into a file it created; it runs without a shell, with the fixed arguments version --json; running it is the purpose of CheckStaged
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = preflightWaitDelay

	return cmd.Run()
}

// capWriter keeps the first limit bytes written to it. Past the limit it records that it was
// exceeded and drops the rest, returning overflow from that Write when overflow is not nil, which
// stops the copy from a process.
type capWriter struct {
	limit    int
	overflow error
	buf      []byte
	exceeded bool
}

// Write implements io.Writer.
func (w *capWriter) Write(p []byte) (int, error) {
	room := w.limit - len(w.buf)
	if len(p) <= room {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}

	w.buf = append(w.buf, p[:room]...)
	w.exceeded = true

	if w.overflow != nil {
		return room, w.overflow
	}

	return len(p), nil
}
