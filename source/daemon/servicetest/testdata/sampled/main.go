// Command sampled is a minimal daemon: it reports READY=1 when systemd listens for it, then
// blocks until SIGTERM or SIGINT and exits 0.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.vipengele.org/daemon/systemd"
)

func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	if _, err := systemd.Notify(os.LookupEnv, systemd.Ready); err != nil {
		fmt.Fprintln(os.Stderr, "sampled:", err)
		os.Exit(1)
	}

	<-stop
}
