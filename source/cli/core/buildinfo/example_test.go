package buildinfo_test

import (
	"encoding/json"
	"fmt"

	"go.vipengele.org/cli/core/buildinfo"
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
	out, err := json.Marshal(info)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
	// Output:
	// {"schema":1,"version":"v1.0.0","commit":"0123abcdef0123abcdef0123abcdef0123abcdef","date":"2026-01-02T03:04:05Z","go":"go1.27.2","os":"linux","arch":"amd64","dev":false}
}
