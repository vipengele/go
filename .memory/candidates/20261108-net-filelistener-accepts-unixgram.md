---
about: "net.FileListener wraps a unixgram socket as a UnixListener without error, so Listeners rejects it by Addr().Network()"
saw:
  - source/daemon/systemd/listeners.go
---

A LISTEN_FDS descriptor from `ListenDatagram=` on a unix socket is not refused by `net.FileListener`: it returns a `*net.UnixListener` whose `Addr().Network()` is "unixgram". `adopt` in source/daemon/systemd/listeners.go checks the network and closes it with "not a stream socket"; a UDP descriptor, by contrast, makes `net.FileListener` itself fail. Either way `listeners` closes every listener opened so far and every remaining inherited descriptor.
