package update_test

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/filelock"
	"go.vipengele.org/update"
	"go.vipengele.org/update/internal/minisigntest"
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
