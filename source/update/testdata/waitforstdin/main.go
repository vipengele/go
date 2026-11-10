// Command waitforstdin stands in for a running executable in the Windows install test. It writes
// "running" and a newline to stdout once it has started, then runs until stdin reaches end of
// file, so the test decides when the process, and the lock Windows keeps on its file, ends. It
// exits 0, or 1 when stdout or stdin fails.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if _, err := io.WriteString(os.Stdout, "running\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
