---
about: "The Supervisor's systemd watchdog runs liveness outside its mutex but pings and re-arms under it, so disarm never waits on a check and nothing is sent after disarm"
saw:
  - source/daemon/supervisor.go
---

`watchdog.arm` in source/daemon/supervisor.go re-arms `Clock.AfterFunc` from each tick (Clock has only Now and AfterFunc). A tick reads `stopped`, runs `live()` (the unexported `health.snapshot(ctx, Liveness)`) without the lock, then re-checks `stopped`, pings and re-arms under the lock. `disarm` sets `stopped` and calls the stored stop func under the same lock.

Moving the ping outside the lock (suggested by a review to avoid an unbounded unixgram write holding `disarm`) would break the guarantee that no WATCHDOG=1 is sent after disarm returns; holding the lock across `live()` would delay the stop phase by up to the Health's check timeout. `systemd.Notify` sets no write deadline.
