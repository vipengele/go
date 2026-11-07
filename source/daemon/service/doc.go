// Package service fronts the operating system's service manager (systemd,
// launchd, the Windows Service Control Manager) through one driver per
// operating system behind the Manager interface.
//
// A service is described by a Config, which New validates before it hands
// back a Manager; a Config that fails validation is reported as
// ErrInvalidConfig. New reports ErrUnsupported for a GOOS with no driver.
//
// The state of an installed service is reported as a Status, whose State is
// one of a fixed set of values that marshal to lower-case text.
//
// # Scopes
//
// A system unit is installed in /etc/systemd/system, driven by systemctl, wanted
// by multi-user.target and ordered after network-online.target. Install,
// Uninstall, Start, Stop and Prepare need root and report
// ErrPrivilegesRequired otherwise. A user unit is installed in
// $XDG_CONFIG_HOME/systemd/user (default $HOME/.config/systemd/user), driven
// by systemctl --user and wanted by default.target; it needs no root, and
// Config.User must be empty. Status reports ErrManagerUnreachable, with
// StateUnknown, when the user manager cannot be reached, as under sudo -u or
// cron.
//
// # Hardening
//
// The systemd driver writes sandboxing directives into the unit.
// DefaultHardening returns the defaults of a scope, and WithSystemdHardening
// replaces them; New validates the value on every GOOS, wrapping
// ErrInvalidConfig, and the other drivers ignore it. A system unit defaults to
// NoNewPrivileges, PrivateTmp, ProtectSystem=full, ProtectHome=read-only,
// ProtectKernelTunables, ProtectControlGroups and RestrictSUIDSGID. A user unit
// defaults to NoNewPrivileges only. An override may add more to a user unit,
// but the sandboxing directives need unprivileged user namespaces; where they
// are unavailable, systemd may refuse to start the unit or skip the directive,
// depending on its version.
//
// # Lingering
//
// The driver never turns lingering on. A user unit then stops when its user's
// last session ends, and Status says so in Detail; "loginctl enable-linger"
// keeps it running without a session.
//
// # Install and updates
//
// Install writes the unit, reloads the manager, enables the unit and restarts
// it. It restarts rather than "enable --now" because the latter leaves a
// running service on its old unit and binary; a restart starts a stopped
// service and replaces a running one, so Install also applies an update.
// Install never creates the account of Config.User; a missing account fails
// before anything is written.
package service
