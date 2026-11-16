package daemoncmd_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon"
	"go.vipengele.org/daemon/daemoncmd"
	"go.vipengele.org/daemon/selfupdate"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/update"
)

// selfUpdater is the Runner of a daemon that updates itself in process. It calls apply once, and
// a daemon passes
//
//	func(ctx context.Context) error { return machine.Apply(ctx, cfg, selection) }
//
// Machine.Apply installs the release, records the update as pending and returns an error wrapping
// daemon.ErrRestartRequired. Returning that error from the Runner ends Supervisor.Run with an
// error for which daemon.RestartRequested is true, so the process exits 0 (a Windows service
// exits with daemon.WindowsRestartExitCode) and its service supervisor starts the installed
// binary. An update that fails any other way is returned as it is and fails the run.
func selfUpdater(apply func(context.Context) error) daemon.Runner {
	return daemon.Func("self-updater", apply)
}

// A daemon that updates itself shares one update.Config and one selfupdate.Machine between the
// update command, the pre-run hooks of the command line and the Supervisor.
func Example_selfUpdate() {
	dir, err := os.MkdirTemp("", "selfupdate-example")
	if err != nil {
		fmt.Println("temp dir:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The Machine keeps the record of the update in a file and knows the running version.
	machine, err := selfupdate.New(selfupdate.NewFileStore(filepath.Join(dir, "update.json")), "v1.0.0")
	if err != nil {
		fmt.Println("machine:", err)
		return
	}

	// One Config, with the Machine as its BackupGuard. It goes to NewUpdate, to Machine.Apply and,
	// through Config.UpdateCheckHooks, to the pre-run hook that removes a stale backup on Windows,
	// which keeps the backup of a pending update only when it holds the guard.
	cfg := update.Config{
		Identity: spec.Identity{
			Name:      "demo",
			Owner:     "vipengele",
			Repo:      "demo",
			EnvPrefix: "DEMO",
			DirName:   "demo",
		},
		// A placeholder for the tool's own minisign public key, which a tool typically embeds from
		// its minisign.pub. This example verifies no release with it.
		TrustedKeys: []string{"RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"},
		Version:     "v1.0.0",
		StateDir:    dir,
		BackupGuard: machine,
	}
	if _, _, err := cfg.UpdateCheckHooks(); err != nil {
		fmt.Println("hooks:", err)
		return
	}

	// The update command installs a release, records it with Machine.Begin and restarts the
	// service that runs the installed binary.
	manager := func(context.Context, *spec.Invocation) (service.Manager, error) {
		return nil, service.ErrUnsupported
	}
	cmds := daemoncmd.NewUpdate(daemoncmd.UpdateConfig{
		Update:        cfg,
		Machine:       machine,
		Manager:       manager,
		ServiceBinary: filepath.Join(dir, "demo"),
	})
	fmt.Println("command:", cmds[0].Name)

	// The Supervisor judges the boot of an updated build before it starts a Runner, rolling the
	// update back when the build is out of attempts, and confirms the update once the run is up.
	// The Runner stands in for the in-process update, which would call Machine.Apply.
	installed := func(context.Context) error {
		return fmt.Errorf("v1.1.0 installed: %w", daemon.ErrRestartRequired)
	}
	sup, err := daemon.New(
		[]daemon.Runner{selfUpdater(installed)},
		daemon.WithUpdateGuard(machine),
	)
	if err != nil {
		fmt.Println("supervisor:", err)
		return
	}

	err = sup.Run(context.Background())
	fmt.Println("restart requested:", daemon.RestartRequested(err))
	// Output:
	// command: update
	// restart requested: true
}
