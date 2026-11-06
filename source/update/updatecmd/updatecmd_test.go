package updatecmd_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/commons/validate"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatecmd"
	"go.vipengele.org/update/updatetest"
)

const (
	testOwner = "pedromvgomes"
	testRepo  = "agentic-toolkit"
	testName  = "atk"
)

// fixture is an executable holding "old" in a directory of its own, an updatetest server
// publishing one signed release of it, and a Config whose executable is that file, whose every
// request goes to the server and which trusts the server's signing key.
type fixture struct {
	exe string
	srv *updatetest.Server
	cfg update.Config
}

// newFixture serves the release tagged served to a Config running version current.
func newFixture(t *testing.T, current, served string, opts ...updatetest.Option) fixture {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	exe := filepath.Join(dir, testName)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rel := updatetest.NewRelease(t, testOwner, testRepo, served, testName)
	srv := updatetest.NewServer(t, rel, opts...)
	home := t.TempDir()

	cfg := update.Config{
		Identity: spec.Identity{
			Name:      testName,
			Owner:     testOwner,
			Repo:      testRepo,
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		Version:     current,
		TrustedKeys: []string{srv.PublicKey()},
		TempDir:     t.TempDir(),
		StateDir:    t.TempDir(),
		HTTPClient:  srv.Client(),
		Executable:  func() (string, error) { return exe, nil },
		InContainer: func() bool { return false },
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		Source: &update.GitHubSource{
			Owner:      testOwner,
			Repo:       testRepo,
			HTTPClient: srv.Client(),
			WebURL:     srv.WebURL(),
			APIURL:     srv.APIURL(),
		},
	}

	return fixture{exe: exe, srv: srv, cfg: cfg}
}

// requests returns how many requests the server has received on every route.
func (f fixture) requests() int {
	n := 0
	for _, target := range []updatetest.Target{
		updatetest.TargetLatest, updatetest.TargetLatestAPI, updatetest.TargetArchive, updatetest.TargetChecksums,
		updatetest.TargetSignature,
	} {
		n += f.srv.Requests(target)
	}
	return n
}

// downloads returns how many requests the server has received for the release's assets.
func (f fixture) downloads() int {
	return f.srv.Requests(updatetest.TargetArchive) + f.srv.Requests(updatetest.TargetChecksums) +
		f.srv.Requests(updatetest.TargetSignature)
}

// binary returns what the executable holds.
func (f fixture) binary(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(f.exe)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(b)
}

// releaseBinary is the content of the binary updatetest.NewRelease packs for tag.
func releaseBinary(tag string) string {
	return "#!/bin/sh\necho " + tag + "\n"
}

// run is one execution of the command: its streams and the error it returned.
type run struct {
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	err    error
}

// runCommand runs the `update` command of cfg with the flag values flags, every one of them
// explicit, stdin as its standard input, and terminal as the answer to every terminal query.
func runCommand(t *testing.T, cfg update.Config, flags map[string]any, stdin string, terminal bool) run {
	t.Helper()
	return runCommandWith(t, context.Background(), cfg, flags, strings.NewReader(stdin), terminal)
}

// runCommandWith is runCommand with the context and the standard input given as they are.
func runCommandWith(t *testing.T, ctx context.Context, cfg update.Config, flags map[string]any, stdin io.Reader, terminal bool) run {
	t.Helper()

	cmds := updatecmd.Commands(cfg)
	if len(cmds) != 1 {
		t.Fatalf("Commands returned %d commands, want 1", len(cmds))
	}

	var stdout, stderr bytes.Buffer
	env := &spec.Env{
		Stdin:    stdin,
		Stdout:   &stdout,
		Stderr:   &stderr,
		Terminal: func(spec.Stream) bool { return terminal },
	}

	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}

	inv := spec.NewInvocation(env, nil, flags, spec.ExplicitFlags(names...))
	err := cmds[0].Run(ctx, inv)
	return run{stdout: &stdout, stderr: &stderr, err: err}
}

// managedBy returns a detector claiming every executable for manager.
func managedBy(manager, instead string) update.Detector {
	return func(string) (update.Install, bool) {
		return update.Install{Manager: manager, Instead: instead}, true
	}
}

// assertPlainError fails t unless err is an error that asks for no exit code and is no usage
// error, so that it exits 1.
func assertPlainError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("Run error = nil, want an error")
	}
	var coded *spec.CodedError
	if errors.As(err, &coded) {
		t.Errorf("Run error %v carries exit code %d, want none", err, coded.Code())
	}
	var usage *spec.UsageError
	if errors.As(err, &usage) {
		t.Errorf("Run error %v is a *spec.UsageError, want a plain error", err)
	}
}

func TestCommandsDeclaresTheUpdateCommand(t *testing.T) {
	t.Parallel()

	cmds := updatecmd.Commands(newFixture(t, "v1.0.0", "v1.0.0").cfg)
	if len(cmds) != 1 {
		t.Fatalf("Commands returned %d commands, want 1", len(cmds))
	}
	cmd := cmds[0]

	if err := cmd.Validate(); err != nil {
		t.Errorf("Validate = %v", err)
	}
	if cmd.Name != "update" {
		t.Errorf("Name = %q, want %q", cmd.Name, "update")
	}
	if _, ok := cmd.Annotations[spec.SkipUpdateCheck]; !ok {
		t.Errorf("Annotations %v lack %q", cmd.Annotations, spec.SkipUpdateCheck)
	}
	if !strings.Contains(cmd.Description, "exit code is 10") {
		t.Errorf("Description does not document exit code 10:\n%s", cmd.Description)
	}

	flags := map[string]string{}
	for _, f := range cmd.Flags {
		flags[f.Name()] = f.Shorthand()
	}
	want := map[string]string{"check": "", "yes": "y", "to": "", "allow-downgrade": ""}
	for name, short := range want {
		got, ok := flags[name]
		if !ok {
			t.Errorf("flag --%s is not declared", name)
			continue
		}
		if got != short {
			t.Errorf("flag --%s shorthand = %q, want %q", name, got, short)
		}
	}
}

func TestCheckReportsANewerRelease(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{"check": true}, "", false)

	var coded *spec.CodedError
	if !errors.As(r.err, &coded) || coded.Code() != updatecmd.ExitUpdateAvailable {
		t.Fatalf("Run error = %v, want one carrying exit code %d", r.err, updatecmd.ExitUpdateAvailable)
	}
	var reported *spec.ReportedError
	if !errors.As(r.err, &reported) {
		t.Errorf("Run error = %v, want it marked as reported", r.err)
	}

	if want := "atk v1.1.0 is available (current v1.0.0)\n"; r.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", r.stdout.String(), want)
	}
	if f.downloads() != 0 {
		t.Errorf("--check made %d download requests, want 0", f.downloads())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestCheckReportsUpToDate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.1.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{"check": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if want := "atk v1.1.0 is up to date\n"; r.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", r.stdout.String(), want)
	}
}

func TestCheckOnAManagedInstallAddsWhatToRunInstead(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.Detectors = []update.Detector{managedBy("Tool", "run `tool upgrade atk`")}
	r := runCommand(t, f.cfg, map[string]any{"check": true}, "", false)

	var coded *spec.CodedError
	if !errors.As(r.err, &coded) || coded.Code() != updatecmd.ExitUpdateAvailable {
		t.Fatalf("Run error = %v, want one carrying exit code %d", r.err, updatecmd.ExitUpdateAvailable)
	}
	want := "atk v1.1.0 is available (current v1.0.0) (managed by Tool: run `tool upgrade atk`)\n"
	if r.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", r.stdout.String(), want)
	}
}

func TestCheckOnAManagedInstallThatIsUpToDateGivesNoHint(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.1.0", "v1.1.0")
	f.cfg.Detectors = []update.Detector{managedBy("Tool", "run `tool upgrade atk`")}
	r := runCommand(t, f.cfg, map[string]any{"check": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if want := "atk v1.1.0 is up to date\n"; r.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", r.stdout.String(), want)
	}
}

func TestADevBuildIsRefused(t *testing.T) {
	t.Parallel()

	for name, flags := range map[string]map[string]any{
		"check":  {"check": true},
		"update": {"yes": true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, "dev", "v1.1.0")
			r := runCommand(t, f.cfg, flags, "", false)

			var dev *update.DevBuildError
			if !errors.As(r.err, &dev) {
				t.Fatalf("Run error = %v, want a *update.DevBuildError", r.err)
			}
			if f.requests() != 0 {
				t.Errorf("server received %d requests, want 0", f.requests())
			}
			if got := f.binary(t); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}
		})
	}
}

func TestAManagedInstallIsNotUpdated(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.Detectors = []update.Detector{managedBy("Tool", "run `tool upgrade atk`")}
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	var managed *update.ManagedInstallError
	if !errors.As(r.err, &managed) {
		t.Fatalf("Run error = %v, want a *update.ManagedInstallError", r.err)
	}
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestUpdateWithoutATerminalNeedsYes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{}, "y\n", false)

	var usage *spec.UsageError
	if !errors.As(r.err, &usage) {
		t.Fatalf("Run error = %v, want a *spec.UsageError", r.err)
	}
	if usage.Flag != "--yes" {
		t.Errorf("UsageError.Flag = %q, want %q", usage.Flag, "--yes")
	}
	if f.downloads() != 0 {
		t.Errorf("server received %d download requests, want 0", f.downloads())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestUpdateDeclinedAtThePromptChangesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{}, "n\n", true)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if !strings.Contains(r.stderr.String(), "Update atk from v1.0.0 to v1.1.0?") {
		t.Errorf("stderr = %q, want the question", r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "update cancelled") {
		t.Errorf("stderr = %q, want %q", r.stderr.String(), "update cancelled")
	}
	if f.downloads() != 0 {
		t.Errorf("server received %d download requests, want 0", f.downloads())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestUpdateAbortedAtThePromptFails(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{}, "", true)

	if !errors.Is(r.err, spec.ErrAborted) {
		t.Fatalf("Run error = %v, want spec.ErrAborted", r.err)
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

// cancelOnRead answers "y" to the first read and cancels its context while doing so, as an
// interrupt arriving while the question waits for its answer does.
type cancelOnRead struct {
	cancel context.CancelFunc
	answer io.Reader
}

func (r *cancelOnRead) Read(p []byte) (int, error) {
	r.cancel()
	return r.answer.Read(p)
}

// attempts records every request a client sends, whether or not it reaches a server, which a
// request with a cancelled context does not.
type attempts struct {
	next http.RoundTripper
	n    atomic.Int64
}

func (a *attempts) RoundTrip(req *http.Request) (*http.Response, error) {
	a.n.Add(1)
	return a.next.RoundTrip(req)
}

func TestUpdateInterruptedAtThePromptDownloadsNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	// The Source keeps the server's own client, so every request through cfg.HTTPClient is a
	// download.
	downloads := &attempts{next: f.srv.Client().Transport}
	f.cfg.HTTPClient = &http.Client{Transport: downloads}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stdin := &cancelOnRead{cancel: cancel, answer: strings.NewReader("y\n")}
	r := runCommandWith(t, ctx, f.cfg, map[string]any{}, stdin, true)

	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", r.err)
	}
	if n := downloads.n.Load(); n != 0 {
		t.Errorf("download requests sent = %d, want 0", n)
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestUpdateConfirmedAtThePromptInstalls(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{}, "y\n", true)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if got, want := f.binary(t), releaseBinary("v1.1.0"); got != want {
		t.Errorf("executable holds %q, want %q", got, want)
	}
}

func TestUpdateWithYesInstallsAndRunsTheHooks(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	var hooked []string
	f.cfg.PostUpdate = []update.PostUpdate{func(path string) error {
		hooked = append(hooked, path)
		return nil
	}}
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if got, want := f.binary(t), releaseBinary("v1.1.0"); got != want {
		t.Errorf("executable holds %q, want %q", got, want)
	}
	if len(hooked) != 1 || hooked[0] != f.exe {
		t.Errorf("hook ran with %v, want [%s]", hooked, f.exe)
	}
	if _, err := os.Stat(f.exe + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat of the backup = %v, want it removed", err)
	}
	if !strings.Contains(r.stderr.String(), "updated atk v1.0.0 → v1.1.0") {
		t.Errorf("stderr = %q, want the update reported", r.stderr.String())
	}
	if r.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", r.stdout.String())
	}
}

func TestAFailingHookIsAWarning(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.PostUpdate = []update.PostUpdate{func(string) error { return errors.New("completion broke") }}
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if !strings.Contains(r.stderr.String(), "completion broke") {
		t.Errorf("stderr = %q, want the hook's failure", r.stderr.String())
	}
	if got, want := f.binary(t), releaseBinary("v1.1.0"); got != want {
		t.Errorf("executable holds %q, want %q", got, want)
	}
}

func TestABackupThatCannotBeRemovedIsAWarning(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	backup := f.exe + ".old"
	// A non-empty directory at the backup's path makes its removal fail, as root too.
	f.cfg.PostUpdate = []update.PostUpdate{func(string) error {
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := os.Mkdir(backup, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(backup, "pinned"), nil, 0o600)
	}}
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if !strings.Contains(r.stderr.String(), "the previous binary is left at "+backup) {
		t.Errorf("stderr = %q, want a warning naming the backup %s", r.stderr.String(), backup)
	}
	if strings.Contains(r.stderr.String(), "a post-update step failed") {
		t.Errorf("stderr = %q, want no hook failure", r.stderr.String())
	}
	if !strings.Contains(r.stderr.String(), "updated atk v1.0.0 → v1.1.0") {
		t.Errorf("stderr = %q, want the update reported", r.stderr.String())
	}
	if got, want := f.binary(t), releaseBinary("v1.1.0"); got != want {
		t.Errorf("executable holds %q, want %q", got, want)
	}
}

func TestUpdateWhenUpToDateDoesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.1.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if !strings.Contains(r.stderr.String(), "atk v1.1.0 is already up to date") {
		t.Errorf("stderr = %q, want the up-to-date message", r.stderr.String())
	}
	if f.downloads() != 0 {
		t.Errorf("server received %d download requests, want 0", f.downloads())
	}
}

func TestToAnOlderReleaseDowngrades(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v2.0.0", "v1.0.0")
	r := runCommand(t, f.cfg, map[string]any{"yes": true, "to": "1.0.0", "allow-downgrade": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if got, want := f.binary(t), releaseBinary("v1.0.0"); got != want {
		t.Errorf("executable holds %q, want %q", got, want)
	}
	if n := f.srv.Requests(updatetest.TargetLatest) + f.srv.Requests(updatetest.TargetLatestAPI); n != 0 {
		t.Errorf("--to asked for the latest release %d times, want 0", n)
	}
}

func TestToAnOlderReleaseWithoutAllowDowngradeIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v2.0.0", "v1.0.0")
	r := runCommand(t, f.cfg, map[string]any{"yes": true, "to": "1.0.0"}, "", false)

	var down *update.DowngradeError
	if !errors.As(r.err, &down) {
		t.Fatalf("Run error = %v, want a *update.DowngradeError", r.err)
	}
	if want := (update.DowngradeError{Name: testName, Version: "v1.0.0", Floor: "v2.0.0"}); *down != want {
		t.Errorf("DowngradeError = %+v, want %+v", *down, want)
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
}

func TestToTheRunningVersionDoesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.0.0")
	r := runCommand(t, f.cfg, map[string]any{"yes": true, "to": "v1.0.0"}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if !strings.Contains(r.stderr.String(), "atk is already at v1.0.0") {
		t.Errorf("stderr = %q, want the no-op message", r.stderr.String())
	}
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestToWithCheckIsAPlainError(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	r := runCommand(t, f.cfg, map[string]any{"check": true, "to": "v1.1.0"}, "", false)

	assertPlainError(t, r.err)
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
}

func TestToANonReleaseVersionIsAPlainError(t *testing.T) {
	t.Parallel()

	for _, to := range []string{"notsemver", "1.2", "v1.2.3+dirty", ""} {
		t.Run(to, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, "v1.0.0", "v1.1.0")
			r := runCommand(t, f.cfg, map[string]any{"yes": true, "to": to}, "", false)

			assertPlainError(t, r.err)
			if f.requests() != 0 {
				t.Errorf("server received %d requests, want 0", f.requests())
			}
		})
	}
}

func TestToAReleaseMissingItsArchiveFails(t *testing.T) {
	t.Parallel()

	archive := updatetest.NewRelease(t, testOwner, testRepo, "v1.1.0", testName).ArchiveName
	f := newFixture(t, "v1.0.0", "v1.1.0", updatetest.MissingAsset(archive))
	r := runCommand(t, f.cfg, map[string]any{"yes": true, "to": "v1.1.0"}, "", false)

	var notFound *update.AssetNotFoundError
	if !errors.As(r.err, &notFound) {
		t.Fatalf("Run error = %v, want a *update.AssetNotFoundError", r.err)
	}
	if notFound.Tag != "v1.1.0" || notFound.Name != archive {
		t.Errorf("AssetNotFoundError = %+v, want tag v1.1.0 and name %q", *notFound, archive)
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
}

func TestAConfigThatDoesNotResolveFails(t *testing.T) {
	t.Parallel()

	for name, flags := range map[string]map[string]any{
		"check":  {"check": true},
		"update": {"yes": true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, "v1.0.0", "v1.1.0")
			f.cfg.MaxExtractBytes = -1
			r := runCommand(t, f.cfg, flags, "", false)

			var field *validate.FieldError
			if !errors.As(r.err, &field) || field.Field != "MaxExtractBytes" {
				t.Fatalf("Run error = %v, want a *validate.FieldError on MaxExtractBytes", r.err)
			}
			var dev *update.DevBuildError
			if errors.As(r.err, &dev) {
				t.Errorf("Run error = %v, want no *update.DevBuildError", r.err)
			}
			if f.requests() != 0 {
				t.Errorf("server received %d requests, want 0", f.requests())
			}
			if got := f.binary(t); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}
		})
	}
}

// staticSource is a Source whose Latest returns rel and err and records the channel it was asked
// for in asked when that is not nil. It is not an update.AssetLocator.
type staticSource struct {
	rel   update.Release
	err   error
	asked *[]update.Channel
}

func (s staticSource) Latest(_ context.Context, ch update.Channel) (update.Release, error) {
	if s.asked != nil {
		*s.asked = append(*s.asked, ch)
	}
	return s.rel, s.err
}

// highestStore is a StateStore holding a State with the given HighestApplied.
type highestStore struct{ highest string }

func (s highestStore) Load(context.Context) (update.State, error) {
	return update.State{HighestApplied: s.highest}, nil
}

func (s highestStore) Save(context.Context, update.State) error { return nil }

func TestChannelFlagReachesTheSource(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		cfgChannel update.Channel
		flags      map[string]any
		want       update.Channel
	}{
		"flag beta":        {update.Stable, map[string]any{"check": true, "channel": "beta"}, update.Beta},
		"flag overrides":   {update.Edge, map[string]any{"check": true, "channel": "stable"}, update.Stable},
		"no flag":          {update.Beta, map[string]any{"check": true}, update.Beta},
		"update with flag": {update.Stable, map[string]any{"yes": true, "channel": "edge"}, update.Edge},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var asked []update.Channel
			f := newFixture(t, "v1.0.0", "v1.1.0")
			f.cfg.Channel = tc.cfgChannel
			f.cfg.Source = staticSource{rel: update.Release{Tag: "v1.0.0"}, asked: &asked}
			r := runCommand(t, f.cfg, tc.flags, "", false)

			if r.err != nil {
				t.Fatalf("Run error = %v, want nil", r.err)
			}
			if len(asked) != 1 || asked[0] != tc.want {
				t.Errorf("Source asked for %v, want [%v]", asked, tc.want)
			}
		})
	}
}

func TestAnUnknownChannelIsAPlainError(t *testing.T) {
	t.Parallel()

	var asked []update.Channel
	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.Source = staticSource{rel: update.Release{Tag: "v1.1.0"}, asked: &asked}
	r := runCommand(t, f.cfg, map[string]any{"yes": true, "channel": "nightly"}, "", false)

	assertPlainError(t, r.err)
	if len(asked) != 0 {
		t.Errorf("Source asked %d times, want 0", len(asked))
	}
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
}

func TestARefusedDowngradeNeverPrompts(t *testing.T) {
	t.Parallel()

	for name, flags := range map[string]map[string]any{
		"pinned": {"to": "v1.5.0"},
		"latest": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, "v1.0.0", "v1.1.0")
			f.cfg.StateStore = highestStore{highest: "v2.0.0"}
			f.cfg.Source = staticSource{rel: update.Release{Tag: "v1.5.0"}}
			stdin := &countingReader{r: strings.NewReader("y\n")}
			r := runCommandWith(t, context.Background(), f.cfg, flags, stdin, true)

			var down *update.DowngradeError
			if !errors.As(r.err, &down) {
				t.Fatalf("Run error = %v, want a *update.DowngradeError", r.err)
			}
			if stdin.reads != 0 {
				t.Errorf("stdin read %d times, want 0", stdin.reads)
			}
			if strings.Contains(r.stderr.String(), "Update atk") {
				t.Errorf("stderr = %q, want no question", r.stderr.String())
			}
			if f.requests() != 0 {
				t.Errorf("server received %d requests, want 0", f.requests())
			}
			if got := f.binary(t); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}
		})
	}
}

// countingReader counts the reads made of r.
type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

func TestCheckWithALatestBelowTheWatermarkIsUpToDate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.StateStore = highestStore{highest: "v2.0.0"}
	f.cfg.Source = staticSource{rel: update.Release{Tag: "v1.5.0"}}
	r := runCommand(t, f.cfg, map[string]any{"check": true}, "", false)

	if r.err != nil {
		t.Fatalf("Run error = %v, want nil", r.err)
	}
	if got, want := r.stdout.String(), "atk v1.0.0 is up to date\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestASourceThatFailsFailsTheRun(t *testing.T) {
	t.Parallel()

	for name, flags := range map[string]map[string]any{
		"check":  {"check": true},
		"update": {"yes": true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			unreachable := errors.New("source unreachable")
			f := newFixture(t, "v1.0.0", "v1.1.0")
			f.cfg.Source = staticSource{err: unreachable}
			r := runCommand(t, f.cfg, flags, "", false)

			if !errors.Is(r.err, unreachable) {
				t.Fatalf("Run error = %v, want %v", r.err, unreachable)
			}
			if r.stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", r.stdout.String())
			}
			if f.requests() != 0 {
				t.Errorf("server received %d requests, want 0", f.requests())
			}
			if got := f.binary(t); got != "old" {
				t.Errorf("executable holds %q, want %q", got, "old")
			}
		})
	}
}

// failingWriter fails every Write with err.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCheckReturnsAFailedWriteOfANewerRelease(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	closed := errors.New("stdout closed")
	var stderr bytes.Buffer
	env := &spec.Env{
		Stdin:    strings.NewReader(""),
		Stdout:   failingWriter{err: closed},
		Stderr:   &stderr,
		Terminal: func(spec.Stream) bool { return false },
	}
	inv := spec.NewInvocation(env, nil, map[string]any{"check": true}, spec.ExplicitFlags("check"))
	err := updatecmd.Commands(f.cfg)[0].Run(context.Background(), inv)

	if !errors.Is(err, closed) {
		t.Fatalf("Run error = %v, want %v", err, closed)
	}
	var coded *spec.CodedError
	if errors.As(err, &coded) {
		t.Errorf("Run error %v carries exit code %d, want none", err, coded.Code())
	}
}

func TestUpdateToAReleaseWithUnlistedAssetsFails(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "v1.0.0", "v1.1.0")
	f.cfg.Source = staticSource{rel: update.Release{Tag: "v1.1.0"}}
	r := runCommand(t, f.cfg, map[string]any{"yes": true}, "", false)

	if !errors.Is(r.err, update.ErrAssetsNotListed) {
		t.Fatalf("Run error = %v, want update.ErrAssetsNotListed", r.err)
	}
	if f.requests() != 0 {
		t.Errorf("server received %d requests, want 0", f.requests())
	}
	if got := f.binary(t); got != "old" {
		t.Errorf("executable holds %q, want %q", got, "old")
	}
	if _, err := os.Stat(f.exe + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat of the backup = %v, want none", err)
	}
}
