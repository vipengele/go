package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
	cfg "go.vipengele.org/config"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatecmd"
)

// snippet:start identity

// identity names the tool: its binary, the GitHub repository its releases are published from,
// the prefix of its environment variables and the directory it owns under the platform's
// configuration, state and cache roots. The repository is a placeholder; a tool names its own.
func identity() spec.Identity {
	return spec.Identity{
		Name:      "greet",
		Owner:     "example",
		Repo:      "greet",
		EnvPrefix: "GREET",
		DirName:   "greet",
	}
}

// snippet:end

// deps are the seams through which the App reaches the outside world. A zero field takes the
// production default of the [update.Config] field of the same name, so main passes [prodDeps] and
// a test passes a release server, a temporary directory and a fixed clock.
type deps struct {
	// Build is the build information "version" prints and the update check compares releases
	// against.
	Build buildinfo.Info
	// Source reports the releases the tool can update to. Nil means the tool's GitHub releases.
	Source update.Source
	// StateDir is the directory the update check keeps its state in. Empty means the tool's
	// state directory.
	StateDir string
	// HTTPClient makes every request of an update. Nil means a client of the update's own.
	HTTPClient *http.Client
	// Now returns the current time. Nil means [time.Now].
	Now func() time.Time
}

// prodDeps returns the deps of the running binary: its own build information and every other
// seam at its default.
func prodDeps() deps {
	return deps{Build: buildinfo.Read()}
}

// snippet:start app

// newApp returns the App of the tool: its identity, the build information "version" prints, the
// configuration [config] describes, the background update check around every command not
// annotated [spec.SkipUpdateCheck], and the commands "hello" and "update".
func newApp(d deps) *cli.App {
	updateCfg := updateConfig(d)
	checkBefore, checkAfter, err := updateCfg.UpdateCheckHooks()
	if err != nil {
		panic(fmt.Sprintf("greet: invalid update configuration: %v", err))
	}

	app := cli.New(identity(),
		cli.WithBuildInfo(d.Build),
		cli.WithConfig(defaultConfig()),
		cli.WithPreRun(checkBefore),
		cli.WithPostRun(checkAfter),
	)
	app.Add(helloCommand())
	for _, cmd := range updatecmd.Commands(updateCfg) {
		app.Add(cmd)
	}
	return app
}

// snippet:end

// snippet:start config

// config is the tool's configuration, read from config.yaml in its configuration directory and
// from the GREET_ environment variables. Its one key, "greeting", is the word a greeting
// starts with.
type config struct {
	Greeting string `doc:"the word a greeting starts with"`
}

// defaultConfig is the configuration before any file or variable sets a key.
func defaultConfig() config {
	return config{Greeting: "Hello"}
}

// Validate refuses an empty greeting, naming the key and where its value came from.
func (c *config) Validate() error {
	if strings.TrimSpace(c.Greeting) == "" {
		return &cfg.FieldError{Key: "greeting", Err: errors.New("must not be empty")}
	}
	return nil
}

// snippet:end

// snippet:start commands

// helloCommand returns "hello [name]", which greets name, or "world" without one, with the
// configured greeting, in capitals under --shout.
func helloCommand() *spec.Command {
	shout := spec.BoolFlag("shout", "greet in capitals")
	return &spec.Command{
		Name:    "hello",
		Summary: "Greet someone",
		Args:    spec.RangeArgs(0, 1),
		Flags:   []spec.Flag{shout},
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			name := "world"
			if len(inv.Args) == 1 {
				name = inv.Args[0]
			}
			greeting := defaultConfig().Greeting
			if loaded, ok := cli.ConfigFrom[config](ctx); ok {
				greeting = loaded.Value().Greeting
			}
			line := greeting + ", " + name + "!"
			if shout.Get(inv) {
				line = strings.ToUpper(line)
			}
			return inv.Data("%s\n", line)
		},
	}
}

// snippet:end

// snippet:start update

// publicKey is the minisign public key the tool's releases are signed with: the contents of its
// minisign.pub. This key belongs to this example alone, and its secret key does not exist, so no
// release ever verifies against it. A tool generates its own pair and embeds its own public key.
const publicKey = `untrusted comment: minisign public key: ED01B5D0CD595CE5
RWTlXFnN0LUB7TK69kkxD61cM8n6bGPEKBYTRyGqdj1OkL2II+9ipoSa`

// updateConfig returns the configuration of the tool's updates. A development build, whose
// version is not a release, starts no background check, and "update" refuses it.
func updateConfig(d deps) update.Config {
	return update.Config{
		Identity:    identity(),
		Version:     d.Build.Version,
		TrustedKeys: []string{publicKey},
		Source:      d.Source,
		StateDir:    d.StateDir,
		HTTPClient:  d.HTTPClient,
		Now:         d.Now,
	}
}

// snippet:end
