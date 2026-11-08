package update_test

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/filelock"
	"go.vipengele.org/update"
	"go.vipengele.org/update/internal/minisigntest"
	"go.vipengele.org/update/updatetest"
)

// exampleKey stands for the contents of the tool's minisign.pub, which a real tool embeds. It is
// drawn from a fixed stream, longer than the key id and Ed25519 seed GenerateKey reads, so every
// call returns the same key.
func exampleKey() string {
	key, err := minisigntest.GenerateKey(strings.NewReader(strings.Repeat("example key ", 8)))
	if err != nil {
		panic(err)
	}
	return key.PublicKey()
}

// A tool refreshes its shell completion after an update by registering [completioninstall.Install]
// as a post-update hook. The hook receives the installed binary's path, and the tool's
// --no-completion flag feeds Options.Disabled.
func Example_completionHook() {
	id := spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	noCompletion := fs.Bool(completioninstall.NoCompletionFlag, false, "do not install shell completion")
	if err := fs.Parse([]string{"--" + completioninstall.NoCompletionFlag}); err != nil {
		fmt.Println(err)
		return
	}

	cfg := update.Config{
		Identity:    id,
		TrustedKeys: []string{exampleKey()},
		PostUpdate: []update.PostUpdate{
			func(installedPath string) error {
				res, err := completioninstall.Install(completioninstall.Options{
					Identity:       id,
					ExecutablePath: installedPath,
					Disabled:       *noCompletion,
				})
				if err != nil {
					return err
				}
				fmt.Printf("skipped=%t reason=%s\n", res.Skipped, res.Reason)
				return nil
			},
		},
	}

	// The tool calls this once the new binary is in place.
	if err := update.RunPostUpdate(cfg.PostUpdate, "/usr/local/bin/atk"); err != nil {
		fmt.Println("completion not refreshed:", err)
	}
	// Output: skipped=true reason=disabled
}

// A tool updates itself with Apply, which refuses a managed install, checks that the executable's
// directory is writable, refuses a release below the highest version applied or running, downloads
// and verifies the release archive, stages the binary beside the running executable, runs it with
// "version --json" and its Preflight hooks to check it is the selected release for the platform,
// swaps it in, keeping the previous binary as a backup, and records the release as the highest
// applied. The tool then runs its post-update hooks on the installed path, reporting a failure
// without undoing the update, and Discard drops the backup. The release's binary here is the POSIX
// shell script of [updatetest.ReleaseBinary], so the example runs on unix only.
func Example_install() {
	dir, err := os.MkdirTemp("", "update-example-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	exe := filepath.Join(dir, "atk")
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		fmt.Println(err)
		return
	}

	// The release's binary answers "version --json" as v2 built for the platform the example runs
	// on, and prints its version when run with no arguments.
	archive, err := updatetest.BuildArchive(updatetest.ExtTarGz, []updatetest.Entry{
		{Name: "atk", Mode: 0o755, Body: updatetest.ReleaseBinary("v2")},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	archiveName := updatetest.ArchiveName("atk", runtime.GOOS, runtime.GOARCH, updatetest.ExtTarGz)
	sums := updatetest.Checksums(map[string][]byte{archiveName: archive})

	// The release's signing key, which a real tool's release pipeline holds and whose public half
	// the tool compiles in.
	key, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Println(err)
		return
	}
	sig := key.Sign(sums, "file:checksums.txt")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = w.Write(sums)
			return
		case "/checksums.txt.minisig":
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	cfg := update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "pedromvgomes",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		TrustedKeys: []string{key.PublicKey()},
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		TempDir:     dir,
		StateDir:    dir,
		Executable:  func() (string, error) { return exe, nil },
		// The example's executable is never a container's, wherever it runs.
		InContainer: func() bool { return false },
		// A Preflight hook vetoes a binary the built-in check accepted, such as one whose database
		// schema the tool cannot migrate to; this one only reports what the binary answered.
		Preflight: []update.Preflight{
			func(_ context.Context, b update.StagedBinary) error {
				fmt.Printf("preflight sees %s, schema %d\n", b.Version, b.Schema)
				return nil
			},
		},
		PostUpdate: []update.PostUpdate{
			func(installedPath string) error {
				out, err := exec.CommandContext(context.Background(), installedPath).Output()
				fmt.Printf("hook runs %s", out)
				return err
			},
		},
	}

	sel := update.Selection{
		Tag:       "v2",
		Archive:   update.Asset{Name: archiveName, URL: srv.URL + "/" + archiveName},
		Checksums: update.Asset{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
		Signature: update.Asset{Name: "checksums.txt.minisig", URL: srv.URL + "/checksums.txt.minisig"},
	}

	installed, err := cfg.Apply(context.Background(), sel)
	if err != nil {
		fmt.Println(err)
		return
	}

	// The update has been applied: a hook that fails is reported, and the new binary stays.
	if err := update.RunPostUpdate(cfg.PostUpdate, installed.Path()); err != nil {
		fmt.Println("post-update:", err)
	}

	// A locked backup (*update.BackupLockedError) is not a failed update and may be ignored.
	_ = installed.Discard()

	_, statErr := os.Stat(installed.Backup())
	fmt.Printf("backup removed=%t\n", os.IsNotExist(statErr))
	// Output:
	// preflight sees v2, schema 1
	// hook runs v2
	// backup removed=true
}

// newerSource is a Source whose latest release is always v1.3.0.
type newerSource struct{}

func (newerSource) Latest(context.Context, update.Channel) (update.Release, error) {
	return update.Release{Tag: "v1.3.0"}, nil
}

// memStore is a StateStore held in memory.
type memStore struct{ st update.State }

func (m *memStore) Load(context.Context) (update.State, error) { return m.st, nil }

func (m *memStore) Save(_ context.Context, st update.State) error {
	m.st = st
	return nil
}

// A tool starts the check when a command begins and finishes it when the command ends. Notice
// reports a release newer than the running version, if the check found one in time.
func ExampleConfig_Start() {
	cfg := update.Config{
		Identity: spec.Identity{
			Name:      "atk",
			Owner:     "pedromvgomes",
			Repo:      "agentic-toolkit",
			EnvPrefix: "ATK",
			DirName:   "agentic-toolkit",
		},
		Version:     "v1.2.0",
		TrustedKeys: []string{exampleKey()},
		Source:      newerSource{},
		StateStore:  &memStore{},
		Locker:      filelock.NewNoop(),
		Now:         func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		Getenv:      func(string) string { return "" },
		IsTerminal:  func() bool { return true },
	}

	check, err := cfg.Start(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	// ... the command runs ...
	check.Finish()
	if n, ok := check.Notice(); ok {
		fmt.Println(n.String())
	}
	// Output: atk v1.3.0 is available (you're on v1.2.0). Run 'atk update' to install.
}
