// Command ldflagsmain prints the text form of [buildinfo.Read] on one line and then the JSON that
// "version --json" writes for it, so that a test can build it with -ldflags and check what the
// linker set.
package main

import (
	"fmt"
	"os"

	"go.vipengele.org/cli/core/buildinfo"
	"go.vipengele.org/cli/spec"
)

func main() {
	info := buildinfo.Read()
	fmt.Println(info.String())
	if err := spec.WriteJSON(os.Stdout, 1, info); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
