---
about: "The systemd driver checks Config.User with `id -u -- <user>` through its runner, not os/user, because a cgo-less os/user reads only /etc/passwd"
saw:
  - source/daemon/service/systemd.go
---

`(*systemd).checkAccount` (source/daemon/service/systemd.go) runs `id -u -- <user>` through the
driver's `run` field before Install writes anything. `os/user` built without cgo parses only
/etc/passwd and misses NSS and LDAP accounts, so it would refuse accounts that systemd can run
as. The driver never creates an account. A missing `id` binary (exec.ErrNotFound) wraps
ErrUnsupported; any other failure names the user and stops Install before a unit is written.
