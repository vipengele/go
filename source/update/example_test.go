package update_test

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
	"go.vipengele.org/update/updatetest"
)

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
		Identity: id,
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

// A tool updates itself in four steps: Download verifies the release archive, Extract stages the
// binary beside the running executable, Install swaps it in keeping the previous binary as a
// backup, and Discard drops the backup once the new binary is accepted. A post-update hook receives
// the installed path.
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

	archive, err := updatetest.BuildArchive(updatetest.ExtTarGz, []updatetest.Entry{
		{Name: "atk", Mode: 0o755, Body: []byte("v2")},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	const archiveName = "atk_linux_amd64.tar.gz"
	sums := updatetest.Checksums(map[string][]byte{archiveName: archive})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksums.txt" {
			_, _ = w.Write(sums)
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
		GOOS:       "linux",
		GOARCH:     "amd64",
		TempDir:    dir,
		StateDir:   dir,
		Executable: func() (string, error) { return exe, nil },
		PostUpdate: []update.PostUpdate{
			func(installedPath string) error {
				body, err := os.ReadFile(installedPath)
				fmt.Printf("hook sees %s\n", body)
				return err
			},
		},
	}

	sel := update.Selection{
		Tag:       "v2",
		Archive:   update.Asset{Name: archiveName, URL: srv.URL + "/" + archiveName},
		Checksums: update.Asset{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
	}

	ctx := context.Background()

	// A best-effort check before the download; Install is the authority.
	if err := cfg.CheckWritable(); err != nil {
		fmt.Println(err)
		return
	}

	arc, err := cfg.Download(ctx, sel)
	if err != nil {
		fmt.Println(err)
		return
	}

	// The staged file goes in the executable's directory, so that the rename stays on one
	// filesystem.
	target, err := cfg.Target()
	if err != nil {
		_ = arc.Close()
		fmt.Println(err)
		return
	}

	staged, err := cfg.Extract(ctx, arc, filepath.Dir(target))
	if err != nil {
		fmt.Println(err)
		return
	}

	installed, err := cfg.Install(staged)
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := update.RunPostUpdate(cfg.PostUpdate, installed.Path()); err != nil {
		// The new binary is unusable: put the previous one back.
		_ = installed.Rollback()
		fmt.Println(err)
		return
	}

	// A locked backup (*update.BackupLockedError) is not a failed update and may be ignored.
	_ = installed.Discard()

	body, _ := os.ReadFile(exe)
	_, statErr := os.Stat(installed.Backup())
	fmt.Printf("installed %s, backup removed=%t\n", body, os.IsNotExist(statErr))
	// Output:
	// hook sees v2
	// installed v2, backup removed=true
}
