package service

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"
)

// Scope selects which service manager domain a service is installed into.
// The zero value is ScopeSystem.
type Scope int

const (
	// ScopeSystem installs the service system-wide, started at boot.
	ScopeSystem Scope = iota
	// ScopeUser installs the service for the current user, started at login.
	ScopeUser
)

const (
	maxNameLen           = 128
	maxPOSIXAccountLen   = 32
	maxWindowsAccountLen = 104
)

// Config describes a service to install. Validate checks it against the
// rules of the operating system it is installed on.
type Config struct {
	// Name identifies the service to the service manager. It is required,
	// starts with an ASCII letter or digit, continues with ASCII letters,
	// digits, '.', '_' or '-', and is at most 128 bytes long.
	Name string
	// Description is human-readable text the service manager shows beside
	// the service. It is optional. The systemd driver drops a trailing run
	// of '\', which would otherwise continue the unit file's line.
	Description string
	// Binary is the absolute path of the executable the service runs. It is
	// required.
	Binary string
	// Args are the arguments passed to Binary, not including the program
	// name.
	Args []string
	// Env holds environment variables set for the service. Each key starts
	// with an ASCII letter or '_' and continues with ASCII letters, digits
	// or '_'. On systemd the values are written into the unit file and are
	// readable by every local account, through the file and through
	// "systemctl show"; they must not carry secrets. Pass a secret another
	// way, such as a file the service reads that only its account may read.
	Env map[string]string
	// WorkingDir is the absolute path of the directory the service runs in.
	// When empty the service manager's default applies. The systemd driver
	// appends '/' to a path ending in '\', naming the same directory, so the
	// unit file's line does not continue onto the next.
	WorkingDir string
	// User is the account the service runs as. When empty the service
	// manager's default for Scope applies. It must be empty with ScopeUser
	// on every GOOS but Windows. On Windows it is an account name
	// with an optional "DOMAIN\" prefix ("." for the local machine); on
	// every other GOOS it is a POSIX user name.
	User string
	// Scope selects a system-wide or a per-user service.
	Scope Scope
}

// Validate reports every way c is unusable on runtime.GOOS, each as an error
// wrapping ErrInvalidConfig, joined with errors.Join. It returns nil for a
// valid Config.
func (c Config) Validate() error {
	return c.validate(runtime.GOOS)
}

// validate checks c against the rules of goos: "" means runtime.GOOS,
// "windows" takes Windows rules and every other value takes POSIX rules.
// Violations are reported in field declaration order, Args by index and Env
// by sorted key, so the joined message is the same on every call.
func (c Config) validate(goos string) error {
	if goos == "" {
		goos = runtime.GOOS
	}
	windows := goos == "windows"

	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalidConfig}, args...)...))
	}

	switch {
	case c.Name == "":
		fail("Name: is required")
	case textProblem(c.Name) != "":
		fail("Name: %s", textProblem(c.Name))
	default:
		if !validName(c.Name) {
			fail("Name: %q must start with an ASCII letter or digit and hold only ASCII letters, digits, '.', '_' or '-'", c.Name)
		}
		if len(c.Name) > maxNameLen {
			fail("Name: is %d bytes long, longer than %d", len(c.Name), maxNameLen)
		}
	}

	if p := textProblem(c.Description); p != "" {
		fail("Description: %s", p)
	}

	switch {
	case c.Binary == "":
		fail("Binary: is required")
	case textProblem(c.Binary) != "":
		fail("Binary: %s", textProblem(c.Binary))
	case !isAbs(c.Binary, windows):
		fail("Binary: %q is not an absolute path on %s", c.Binary, goos)
	}

	for i, a := range c.Args {
		if p := textProblem(a); p != "" {
			fail("Args[%d]: %s", i, p)
		}
	}

	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if p := textProblem(k); p != "" {
			fail("Env[%q]: key %s", k, p)
		} else if !validEnvKey(k) {
			fail("Env[%q]: key must start with an ASCII letter or '_' and hold only ASCII letters, digits or '_'", k)
		}
		if p := textProblem(c.Env[k]); p != "" {
			fail("Env[%q]: value %s", k, p)
		}
	}

	if c.WorkingDir != "" {
		if p := textProblem(c.WorkingDir); p != "" {
			fail("WorkingDir: %s", p)
		} else if !isAbs(c.WorkingDir, windows) {
			fail("WorkingDir: %q is not an absolute path on %s", c.WorkingDir, goos)
		}
	}

	if c.User != "" {
		switch {
		case textProblem(c.User) != "":
			fail("User: %s", textProblem(c.User))
		case windows && !validWindowsAccount(c.User):
			fail("User: %q is not a Windows account name", c.User)
		case !windows && !validPOSIXAccount(c.User):
			fail("User: %q is not a POSIX user name", c.User)
		}
	}

	if !windows && c.Scope == ScopeUser && c.User != "" {
		fail("User: %q cannot be set with ScopeUser, a user service runs as the user who installs it", c.User)
	}

	if c.Scope != ScopeSystem && c.Scope != ScopeUser {
		fail("Scope: %d is neither ScopeSystem nor ScopeUser", int(c.Scope))
	}

	return errors.Join(errs...)
}

// textProblem describes why s cannot be passed to a service manager, or
// returns "" when it can. A control character (U+0000 to U+001F, U+007F)
// would let a value break out of the line or field a driver writes it into,
// and invalid UTF-8 has no faithful representation in any of them.
func textProblem(s string) string {
	for i, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size <= 1 {
				return fmt.Sprintf("holds invalid UTF-8 at byte %d", i)
			}
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Sprintf("holds control character %U at byte %d", r, i)
		}
	}
	return ""
}

func isASCIILetter(b byte) bool { return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

func isASCIILower(b byte) bool { return 'a' <= b && b <= 'z' }

func isASCIIDigit(b byte) bool { return '0' <= b && b <= '9' }

// validName reports whether s matches [A-Za-z0-9][A-Za-z0-9._-]*.
func validName(s string) bool {
	if s == "" || !isASCIILetter(s[0]) && !isASCIIDigit(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		b := s[i]
		if !isASCIILetter(b) && !isASCIIDigit(b) && b != '.' && b != '_' && b != '-' {
			return false
		}
	}
	return true
}

// validEnvKey reports whether s matches [A-Za-z_][A-Za-z0-9_]*.
func validEnvKey(s string) bool {
	if s == "" || !isASCIILetter(s[0]) && s[0] != '_' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isASCIILetter(s[i]) && !isASCIIDigit(s[i]) && s[i] != '_' {
			return false
		}
	}
	return true
}

// isAbs reports whether p is absolute under the target's rules, independent
// of the host: on Windows a drive path (`C:\`, `C:/`) or a UNC path
// (`\\server\share`), elsewhere a path starting with '/'.
func isAbs(p string, windows bool) bool {
	if !windows {
		return strings.HasPrefix(p, "/")
	}
	if len(p) >= 3 && isASCIILetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	rest, ok := strings.CutPrefix(p, `\\`)
	if !ok {
		return false
	}
	server, after, ok := strings.Cut(rest, `\`)
	if !ok || server == "" {
		return false
	}
	share, _, _ := strings.Cut(after, `\`)
	return share != ""
}

// validPOSIXAccount reports whether s is a portable POSIX user name: a
// lower-case letter or '_', then lower-case letters, digits, '_' or '-',
// with an optional trailing '$', at most 32 bytes in all.
func validPOSIXAccount(s string) bool {
	if s == "" || len(s) > maxPOSIXAccountLen {
		return false
	}
	body := strings.TrimSuffix(s, "$")
	if body == "" || !isASCIILower(body[0]) && body[0] != '_' {
		return false
	}
	for i := 1; i < len(body); i++ {
		b := body[i]
		if !isASCIILower(b) && !isASCIIDigit(b) && b != '_' && b != '-' {
			return false
		}
	}
	return true
}

// validWindowsAccount reports whether s is a Windows account name with an
// optional "DOMAIN\" prefix, at most 104 bytes in all. Domain and name each
// hold ASCII letters, digits, ' ', '.', '_' or '-', are non-empty and neither
// start nor end with a space; "." as the domain names the local machine.
func validWindowsAccount(s string) bool {
	if len(s) > maxWindowsAccountLen {
		return false
	}
	domain, name, hasDomain := strings.Cut(s, `\`)
	if !hasDomain {
		return validWindowsAccountPart(s)
	}
	return validWindowsAccountPart(domain) && validWindowsAccountPart(name)
}

func validWindowsAccountPart(s string) bool {
	if s == "" || s[0] == ' ' || s[len(s)-1] == ' ' {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !isASCIILetter(b) && !isASCIIDigit(b) && b != ' ' && b != '.' && b != '_' && b != '-' {
			return false
		}
	}
	return true
}
