package systemd_test

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"go.vipengele.org/daemon/systemd"
)

// A process started outside systemd has no NOTIFY_SOCKET, and Notify sends nothing.
func ExampleNotify() {
	outside := func(string) (string, bool) { return "", false }

	sent, err := systemd.Notify(outside, systemd.Ready)
	fmt.Println(sent, err)
	// Output: false <nil>
}

// A service pings at half the interval systemd reports, so that a late ping still arrives in
// time.
func ExampleWatchdogInterval() {
	interval, enabled, err := systemd.WatchdogInterval(os.LookupEnv)
	if err != nil {
		log.Fatal(err)
	}
	if !enabled {
		return
	}
	ticker := time.NewTicker(interval / 2)
	defer ticker.Stop()
	for range ticker.C {
		if _, err := systemd.Notify(os.LookupEnv, systemd.Watchdog); err != nil {
			log.Print(err)
		}
	}
}

// A daemon serves on the socket systemd passed it as "web", and listens itself when it was not
// socket-activated.
func ExampleListeners() {
	inherited, err := systemd.Listeners(os.LookupEnv)
	if err != nil {
		log.Fatal(err)
	}
	var l net.Listener
	if web := inherited["web"]; len(web) > 0 {
		l = web[0]
	} else if l, err = net.Listen("tcp", "127.0.0.1:8080"); err != nil {
		log.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	fmt.Println("serving on", l.Addr())
}
