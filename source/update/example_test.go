package update_test

import (
	"flag"
	"fmt"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/completioninstall"
	"go.vipengele.org/update"
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
