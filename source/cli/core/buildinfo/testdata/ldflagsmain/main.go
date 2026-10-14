// Command ldflagsmain prints the text form of [buildinfo.Read] on one line and its JSON encoding on
// the next, so that a test can build it with -ldflags and check what the linker set.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"go.vipengele.org/cli/core/buildinfo"
)

func main() {
	info := buildinfo.Read()
	b, err := json.Marshal(info)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(info.String())
	fmt.Println(string(b))
}
