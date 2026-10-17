package state_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.vipengele.org/cli/state"
)

type settings struct {
	Theme   string `json:"theme"   yaml:"theme"`
	Retries int    `json:"retries" yaml:"retries"`
}

func ExampleLoadJSON() {
	dir, err := os.MkdirTemp("", "state-example-")
	if err != nil {
		fmt.Println(err)

		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "settings.json")

	_, status, err := state.LoadJSON[settings](path)
	fmt.Println(status, err)

	if err := os.WriteFile(path, []byte(`{"theme": "dark", "retries": 2}`), 0o600); err != nil {
		fmt.Println(err)

		return
	}

	s, status, err := state.LoadJSON[settings](path)
	fmt.Println(status, err, s.Theme, s.Retries)

	// Output:
	// missing <nil>
	// loaded <nil> dark 2
}

func ExampleSaveJSON() {
	dir, err := os.MkdirTemp("", "state-example-")
	if err != nil {
		fmt.Println(err)

		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "settings.json")

	if err := state.SaveJSON(path, settings{Theme: "dark", Retries: 2}); err != nil {
		fmt.Println(err)

		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Print(string(data))

	// Output:
	// {
	//   "theme": "dark",
	//   "retries": 2
	// }
}

func ExampleLoadYAML() {
	dir, err := os.MkdirTemp("", "state-example-")
	if err != nil {
		fmt.Println(err)

		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "settings.yaml")

	if err := os.WriteFile(path, []byte("theme: [dark\n"), 0o600); err != nil {
		fmt.Println(err)

		return
	}

	_, status, err := state.LoadYAML[settings](path)
	fmt.Println(status, errors.Is(err, state.ErrCorrupt))

	if err := os.WriteFile(path, []byte("theme: dark\nretries: 2\n"), 0o600); err != nil {
		fmt.Println(err)

		return
	}

	s, status, err := state.LoadYAML[settings](path)
	fmt.Println(status, err, s.Theme, s.Retries)

	// Output:
	// corrupt true
	// loaded <nil> dark 2
}

func ExampleSaveYAML() {
	dir, err := os.MkdirTemp("", "state-example-")
	if err != nil {
		fmt.Println(err)

		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "settings.yaml")

	if err := state.SaveYAML(path, settings{Theme: "dark", Retries: 2}); err != nil {
		fmt.Println(err)

		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Print(string(data))

	// Output:
	// theme: dark
	// retries: 2
}
