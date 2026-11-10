---
about: "systemd.Listeners closes the inherited descriptors it adopts, so its success path is tested in a re-exec'd child with ExtraFiles, never in-process"
saw:
  - source/daemon/systemd/listeners.go
  - source/daemon/systemd/listeners_test.go
---

`adopt` in source/daemon/systemd/listeners.go wraps each LISTEN_FDS descriptor with `net.FileListener` (which dups it) and then closes the original descriptor. Feeding it `(*net.TCPListener).File().Fd()` in-process leaves the test's `*os.File` owning a number `adopt` already closed, so its later Close is a double close that can hit a reused descriptor: a flaky test.

source/daemon/systemd/listeners_test.go therefore re-runs its own test binary with the sockets in `cmd.ExtraFiles` (fds 3 and up, as systemd passes them); the child is `TestListenersChild`, gated on `VIPENGELE_SYSTEMD_TEST_CHILD`. The child counts sockets in /proc/self/fd to catch leaks. Consequence: the success path of `adopt` does not show in the parent's coverage profile.
