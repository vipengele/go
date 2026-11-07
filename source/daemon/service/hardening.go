package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Hardening is the set of sandboxing directives the systemd driver writes into
// a unit. Other drivers ignore it.
type Hardening struct {
	// NoNewPrivileges stops the service and its children from gaining
	// privileges through setuid or file capabilities.
	NoNewPrivileges bool
	// PrivateTmp gives the service private /tmp and /var/tmp directories.
	PrivateTmp bool
	// ProtectSystem makes the file system hierarchy read-only for the
	// service. It is "", "true", "full" or "strict".
	ProtectSystem string
	// ProtectHome restricts access to the home directories. It is "",
	// "true", "read-only" or "tmpfs".
	ProtectHome string
	// ProtectKernelTunables makes kernel variables such as /proc/sys
	// read-only.
	ProtectKernelTunables bool
	// ProtectControlGroups makes the control group hierarchies read-only.
	ProtectControlGroups bool
	// RestrictSUIDSGID stops the service from creating setuid or setgid files.
	RestrictSUIDSGID bool
	// ReadWritePaths are the absolute POSIX paths that stay writable when
	// ProtectSystem or ProtectHome would otherwise make them read-only.
	ReadWritePaths []string
}

// DefaultHardening returns the hardening a systemd unit of scope gets without
// WithSystemdHardening. A system unit gets the full set; a user unit gets
// NoNewPrivileges only, because the sandboxing directives that need
// privileges do not apply to a user manager.
func DefaultHardening(scope Scope) Hardening {
	if scope == ScopeUser {
		return Hardening{NoNewPrivileges: true}
	}
	return Hardening{
		NoNewPrivileges:       true,
		PrivateTmp:            true,
		ProtectSystem:         "full",
		ProtectHome:           "read-only",
		ProtectKernelTunables: true,
		ProtectControlGroups:  true,
		RestrictSUIDSGID:      true,
	}
}

// validate reports every violation in h, each wrapping ErrInvalidConfig,
// joined with errors.Join in field declaration order and ReadWritePaths by
// index. The rules do not depend on the target GOOS.
func (h Hardening) validate() error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalidConfig}, args...)...))
	}
	if !slices.Contains([]string{"", "true", "full", "strict"}, h.ProtectSystem) {
		fail("Hardening.ProtectSystem: %q is not one of \"\", \"true\", \"full\", \"strict\"", h.ProtectSystem)
	}
	if !slices.Contains([]string{"", "true", "read-only", "tmpfs"}, h.ProtectHome) {
		fail("Hardening.ProtectHome: %q is not one of \"\", \"true\", \"read-only\", \"tmpfs\"", h.ProtectHome)
	}
	for i, p := range h.ReadWritePaths {
		switch {
		case !strings.HasPrefix(p, "/"):
			fail("Hardening.ReadWritePaths[%d]: %q is not an absolute path", i, p)
		case strings.IndexFunc(p, isControl) >= 0:
			fail("Hardening.ReadWritePaths[%d]: %q contains a control character", i, p)
		}
	}
	return errors.Join(errs...)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }
