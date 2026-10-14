package paths_test

import (
	"errors"
	"fmt"
	"path/filepath"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
)

func ExampleNew() {
	id := spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
	env := map[string]string{"XDG_CACHE_HOME": "/var/cache"}

	// A test or a tool pins the layout, the environment and the home directory; the zero
	// Options reads them from the process.
	dirs, err := paths.New(id, paths.Options{
		GOOS:        "linux",
		StateDir:    "/srv/atk/state",
		LookupEnv:   func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		UserHomeDir: func() (string, error) { return "/home/ada", nil },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, dir := range []string{dirs.Config(), dirs.State(), dirs.Cache(), dirs.Data(), dirs.Log()} {
		fmt.Println(filepath.ToSlash(dir))
	}
	// Output:
	// /home/ada/.config/agentic-toolkit
	// /srv/atk/state
	// /var/cache/agentic-toolkit
	// /home/ada/.local/share/agentic-toolkit
	// /srv/atk/state/log
}

func ExampleResolve() {
	id := spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
	env := map[string]string{"XDG_STATE_HOME": "/var/lib/state"}

	// Only the state directory is resolved, so a home directory that cannot be found does not
	// matter: every other kind is left unresolved.
	dirs, err := paths.Resolve(id, paths.Options{
		GOOS:        "linux",
		LookupEnv:   func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		UserHomeDir: func() (string, error) { return "", errors.New("no home") },
	}, paths.State)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(filepath.ToSlash(dirs.State()))
	fmt.Printf("%q\n", dirs.Config())
	// Output:
	// /var/lib/state/agentic-toolkit
	// ""
}
