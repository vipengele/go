package buildinfo_test

import (
	"fmt"
	"os"

	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

func ExampleInfo_String() {
	release := buildinfo.Info{Version: "v1.0.0", Commit: "0123abcdef0123abcdef0123abcdef0123abcdef"}
	fmt.Println(release)
	fmt.Println(buildinfo.Info{Version: "v1.0.0"})
	fmt.Println(buildinfo.Info{})
	// Output:
	// v1.0.0 build 0123abcdef01
	// v1.0.0
	// dev
}

func ExampleInfo_MarshalJSON() {
	info := buildinfo.Info{
		Version:   "v1.0.0",
		Commit:    "0123abcdef0123abcdef0123abcdef0123abcdef",
		Date:      "2026-01-02T03:04:05Z",
		GoVersion: "go1.27.2",
		OS:        "linux",
		Arch:      "amd64",
	}
	// The version command writes the Info under schema 1, as "version --json" prints it.
	if err := spec.WriteJSON(os.Stdout, 1, info); err != nil {
		fmt.Println(err)
	}
	// Output:
	// {
	//   "schema": 1,
	//   "version": "v1.0.0",
	//   "commit": "0123abcdef0123abcdef0123abcdef0123abcdef",
	//   "date": "2026-01-02T03:04:05Z",
	//   "go": "go1.27.2",
	//   "os": "linux",
	//   "arch": "amd64",
	//   "dev": false
	// }
}
