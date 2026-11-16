# Building a command-line tool on the vipengele libraries

This guide builds a command-line tool on `go.vipengele.org/cli/core`, `go.vipengele.org/cli/spec`,
`go.vipengele.org/config` and `go.vipengele.org/update`: an App with a command of its own, a
configuration file, a self-update, and signed releases published from GitHub.

Every snippet comes from [`source/examples/greet`](../../source/examples/greet), a small tool that
requires these modules at their tags exactly as a consumer does. The example's tests compare each
snippet with the source it quotes, so what you read here builds and runs. The example is never
tagged and is not meant for `go get`: read it, run it, and copy from it. Wherever it names itself
(`greet`, `GREET`, the repository `example/greet`, its public key), substitute your own tool's.

## Prerequisites

Go 1.25 or later. The modules resolve through `go.vipengele.org`, which answers with a four-field
`go-import` tag; earlier Go releases ignore its fourth field and cannot find the modules.

In your tool's module, require the four modules:

```bash
go get go.vipengele.org/cli/core@v0.1.0 go.vipengele.org/cli/spec@v0.1.0 go.vipengele.org/config@v0.1.0 go.vipengele.org/update@v0.1.0
```

Releasing (step 5) also needs [minisign](https://jedisct1.github.io/minisign/) on your machine,
to generate the signing key.

## 1. Create the App

A tool is named by a `spec.Identity`: its binary name, the GitHub repository its releases are
published from, the prefix of its environment variables and the directory it owns under the
platform's configuration, state and cache roots.

<!-- snippet: app.go#identity -->

```go
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
```

`cli.New` builds the App from the identity and the options the tool wants, and `App.Add` adds the
tool's commands. The App always holds `version`, which prints the build information
`buildinfo.Read` returns; a tool never adds a command of that name. The example's `newApp` is the
whole App, each piece of which the steps below explain:

<!-- snippet: app.go#app -->

```go
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
```

`deps` gathers the seams through which the App reaches the outside world. `main` passes
`prodDeps()`, the running binary's `buildinfo.Read()` with every other seam at its default, and a
test passes a release server, a temporary directory and a fixed clock instead (see the example's
`app_test.go`). `App.Execute` reads the process's arguments, standard streams and environment, and
returns the exit code:

<!-- snippet: main.go -->

```go
package main

import "os"

func main() {
	os.Exit(newApp(prodDeps()).Execute())
}
```

## 2. Add commands

A command is a `spec.Command`: a name, a summary, an argument policy, its flags and a run
function. It depends on `cli/spec` alone, never on the framework that parses the command line.

<!-- snippet: app.go#commands -->

```go
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
```

The flag handle `shout` reads its value from the `Invocation` with `Get`. `inv.Data` writes to the
standard output, which carries the command's data; messages go to the standard error. A command
nests others through its `Commands` field, and the App checks the whole tree before every run.

## 3. Add configuration

A configuration is a struct: each exported field is a key, a `doc` tag describes it, and the value
passed to `cli.WithConfig` holds the default of every key. A `Validate` method on the type is
called once every source has loaded; a problem with one key is a `*config.FieldError` naming it.

<!-- snippet: app.go#config -->

```go
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
```

`cli.WithConfig(defaultConfig())` (step 1) gives the App:

- a hook, run before every other, that loads the defaults, then `config.yaml` in the tool's
  configuration directory, then one environment variable per key, named from the identity's
  `EnvPrefix`: `GREET_GREETING` for `greeting`. Each later source overrides the earlier ones. A
  missing file is valid. A run function reads the result with `cli.ConfigFrom[config](ctx)`.
- the `config` command group: `config path`, `config show`, `config validate` and `config edit`.

The configuration directory follows the platform: `$XDG_CONFIG_HOME/greet` or `~/.config/greet` on
Linux, `~/Library/Application Support/greet` on macOS, `%AppData%\greet` on Windows, or
`$GREET_HOME/config` when that variable is set. `greet config path` prints the file's path.

A configuration that fails to load or validate stops the command with exit code 1 and names the
key, its value and its origin:

```bash
$ GREET_GREETING=" " greet hello
greet: load config
  → greeting: must not be empty: " " from env GREET_GREETING
```

## 4. Wire update

`update.Config` describes where the tool's releases come from and how they are trusted. The
identity names the GitHub repository, `Version` is the running build's version, and `TrustedKeys`
holds the minisign public key the releases are signed with (step 5 generates it). Leaving
`Source`, `StateDir`, `HTTPClient` and `Now` zero takes their production defaults: the tool's
GitHub releases, its state directory, a client of the update's own and `time.Now`.

<!-- snippet: app.go#update -->

```go
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
```

A `Config` with no `TrustedKeys` is invalid unless it sets `AllowUnsigned`, and one that sets both
is invalid too: a tool states one signature policy. Keys come only from code, never from a flag,
a variable or the configuration file.

`newApp` (step 1) wires the `Config` twice:

- `updatecmd.Commands(updateCfg)` returns the `update` command, which replaces the running binary
  with the latest release, or the one `--to` names, after a confirmation `--yes` answers in
  advance; `update --check` only reports whether a newer release exists.
- `updateCfg.UpdateCheckHooks()` returns the hooks of the background update check, which
  `cli.WithPreRun` and `cli.WithPostRun` run around every command. The check never fails a command:
  when it finds a newer release, it writes a one-line notice to the standard error after the
  command. It skips a command annotated `spec.SkipUpdateCheck`, as `update` and `version` are; a
  command of the tool's own opts out by carrying the key in its `Annotations`. It returns an error
  only for an invalid `Config`, which is a programming error, hence the panic.

The check starts nothing on a development build, whose version is not a release version, in CI,
when the standard error is not a terminal, or within the check interval of the last attempt; and
`update` refuses a development build. A binary built by `go build` is one, and `greet version`
says `dev build`. Only a binary built by the release workflow below, with the tag linked in, checks for and applies
updates.

## 5. Release

A release is a `vX.Y.Z` tag pushed to the tool's repository. Its workflow calls the reusable
workflow `release-tool.yml` of `vipengele/go`, which builds every platform with GoReleaser, signs
`checksums.txt` with minisign and publishes a GitHub Release. Before the first tag, set up the key,
the GoReleaser config and the workflow once.

### The signing key

Generate a key pair without a password; the release workflow signs unattended:

```bash
minisign -G -W -p minisign.pub -s minisign.key
```

Embed the contents of `minisign.pub` as the tool's `publicKey` (step 4). Keep `minisign.key` out
of the repository, and offline once it is stored as a secret.

In the tool's GitHub repository, create an environment named `release`, protect it so only `v*`
tags can deploy to it, and add a secret `MINISIGN_KEY` to it holding the whole contents of
`minisign.key`. The workflow writes the secret to `minisign.key` in the build job, signs with it
and deletes it.

### The GoReleaser config

The GoReleaser config is rendered from the tool's `update.Config`, so the archives, checksums file
and signature a release publishes carry the names `update` selects and verifies. Render it with
`goreleaser.Render` of `go.vipengele.org/update/goreleaser`, passing the `Config` and the tool's
main package: `goreleaser.Render(updateConfig(prodDeps()), goreleaser.Options{Main: "./"})`
returns the file's bytes. Commit them as `.goreleaser.yaml` at the root of the repository, and keep
a golden test that fails when the committed file differs from the rendered one, as the example's
[`release_test.go`](../../source/examples/greet/release_test.go) does; its `-update` flag rewrites
the file:

```bash
go test -run TestReleaseGoreleaserConfig . -update
```

The example's `.goreleaser.yaml`:

<!-- snippet: .goreleaser.yaml -->

```yaml
# GoReleaser v2.6 or later. Rendered by go.vipengele.org/update/goreleaser from the tool's
# update.Config: the build, archive, checksum and signature names below are the ones update
# selects, verifies and extracts.
version: 2
project_name: "greet"
builds:
  - binary: "greet"
    main: "./"
    env: ["CGO_ENABLED=0"]
    ldflags: ["-s -w", "-X go.vipengele.org/cli/core/buildinfo.version={{ .Tag }}", "-X go.vipengele.org/cli/core/buildinfo.commit={{ .FullCommit }}", "-X go.vipengele.org/cli/core/buildinfo.date={{ .Date }}"]
    goos: ["linux", "darwin"]
    goarch: ["amd64", "arm64"]
    ignore: []
archives:
  - name_template: "greet_{{ .Os }}_{{ .Arch }}"
    formats: ["tar.gz"]
checksum:
  name_template: "checksums.txt"
  algorithm: sha256
signs:
  - artifacts: checksum
    cmd: minisign
    args: ["-S", "-s", "minisign.key", "-m", "${artifact}", "-x", "${signature}"]
    signature: "${artifact}.minisig"
```

The ldflags link the tag, `v` included, into `buildinfo`, which makes the binary a release build
that checks for updates. Re-render the file whenever the `update.Config` or the `update` require
changes.

### The release notes

The GitHub Release body is a file the workflow requires before it builds:
`docs/release-notes/<tag>.md` in the tool's repository, `docs/release-notes/v0.1.0.md` for the tag
`v0.1.0`. Commit it before pushing the tag.

### The workflow

Add `.github/workflows/release.yml` to the tool's repository:

<!-- snippet: release.yml#release -->

```yaml
name: release

on:
  push:
    tags: ['v*']

permissions: {}

jobs:
  release:
    # GitHub checks every job of the called workflow against this grant when the
    # run starts, and its publish job creates the release.
    permissions:
      contents: write
    uses: vipengele/go/.github/workflows/release-tool.yml@415a61a829b6d2e615e0ca978023b315d7588904 # source/update/v0.1.0
    with:
      environment: release
    secrets:
      MINISIGN_KEY: ${{ secrets.MINISIGN_KEY }}
```

- `uses` pins `release-tool.yml` at the commit the tag `source/update/vX.Y.Z` points to, where
  `vX.Y.Z` is the version of the tool's `go.vipengele.org/update` require, with the tag as a
  trailing comment. The workflow is released with `update`, so it builds exactly the config
  `goreleaser.Render` of that version writes. The pin is a commit, not a tag, because the called
  build job holds the signing key and a tag can be moved. Find the commit with:

  ```bash
  git ls-remote https://github.com/vipengele/go 'refs/tags/source/update/v0.1.0^{}'
  ```

  When the tool raises its `update` require, it moves the pin and the comment to the new tag and
  re-renders `.goreleaser.yaml`.
- `contents: write` is required. GitHub checks every job of the called workflow against the
  caller's grant when the run starts, and the publish job creates the release, so a lesser grant
  fails the run before it builds.
- `environment: release` names the environment holding `MINISIGN_KEY`; the called build job
  resolves the secret there.

`release-tool.yml` takes further inputs for a tool laid out differently: `working-directory`, and
relative to it `goreleaser-config` (default `.goreleaser.yaml`), `release-notes-dir` (default
`docs/release-notes`), `signing-key-path` (default `minisign.key`) and `go-version-file` (default
`go.mod`). A tool whose `update.Config` sets `AllowUnsigned` passes `sign: false` and needs no key.

### Push the tag

With the notes committed on the default branch, tag the commit and push the tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds the tag, signs `checksums.txt`, and publishes the release with the archives,
`checksums.txt` and `checksums.txt.minisig`. A tag with a semver pre-release suffix, such as
`v0.2.0-rc.1`, is published as a GitHub pre-release. Re-running a failed run finishes a partial
release. From then on, every installed release build of the tool finds the next release through its
background check, and `update` installs it.
