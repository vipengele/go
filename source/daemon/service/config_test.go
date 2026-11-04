package service

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// validConfig returns a Config every check accepts on goos.
func validConfig(goos string) Config {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return Config{
			Name:        "my-svc",
			Description: "My service",
			Binary:      `C:\Program Files\svc\svc.exe`,
			Args:        []string{"--port", "8080"},
			Env:         map[string]string{"LOG_LEVEL": "debug", "_X": "100%"},
			WorkingDir:  `C:\ProgramData\svc`,
			User:        `NT AUTHORITY\LocalService`,
			Scope:       ScopeSystem,
		}
	}
	return Config{
		Name:        "my-svc",
		Description: "My service",
		Binary:      "/usr/local/bin/svc",
		Args:        []string{"--port", "8080", "a b"},
		Env:         map[string]string{"LOG_LEVEL": "debug", "_X": "100%"},
		WorkingDir:  "/var/lib/svc",
		User:        "svc",
		Scope:       ScopeUser,
	}
}

// violations returns the errors joined into err, or nil for a nil err.
func violations(t *testing.T, err error) []error {
	t.Helper()
	if err == nil {
		return nil
	}
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("error %v is not a joined error", err)
	}
	errs := j.Unwrap()
	for _, e := range errs {
		if !errors.Is(e, ErrInvalidConfig) {
			t.Errorf("violation %q does not match ErrInvalidConfig", e)
		}
	}
	return errs
}

func TestValidateAcceptsAValidConfigOnEveryGOOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "plan9", ""} {
		t.Run("goos="+goos, func(t *testing.T) {
			c := validConfig(goos)
			if err := c.validate(goos); err != nil {
				t.Fatalf("validate(%q) = %v, want nil", goos, err)
			}
			c.Args, c.Env, c.Description, c.WorkingDir, c.User = nil, nil, "", "", ""
			if err := c.validate(goos); err != nil {
				t.Fatalf("validate(%q) with only required fields = %v, want nil", goos, err)
			}
		})
	}
}

func TestValidateChecksAbsolutePathsByTargetGOOS(t *testing.T) {
	tests := []struct {
		goos string
		path string
		ok   bool
	}{
		{"linux", "/usr/bin/x", true},
		{"darwin", "/usr/bin/x", true},
		{"plan9", "/bin/x", true},
		{"linux", `C:\x`, false},
		{"linux", "C:/x", false},
		{"linux", "bin/x", false},
		{"linux", "./x", false},
		{"windows", "/usr/bin/x", false},
		{"windows", "bin/x", false},
		{"windows", `bin\x`, false},
		{"windows", `C:x`, false},
		{"windows", `1:\x`, false},
		{"windows", `\x`, false},
		{"windows", `C:/x`, true},
		{"windows", `C:\x`, true},
		{"windows", `c:\x`, true},
		{"windows", `\\srv\share\x`, true},
		{"windows", `\\srv\share`, true},
		{"windows", `\\srv`, false},
		{"windows", `\\srv\`, false},
		{"windows", `\\\share`, false},
	}
	for _, tc := range tests {
		for _, field := range []string{"Binary", "WorkingDir"} {
			t.Run(tc.goos+"/"+field+"/"+tc.path, func(t *testing.T) {
				c := validConfig(tc.goos)
				if field == "Binary" {
					c.Binary = tc.path
				} else {
					c.WorkingDir = tc.path
				}
				errs := violations(t, c.validate(tc.goos))
				if tc.ok {
					if len(errs) != 0 {
						t.Fatalf("validate(%q) rejected %s %q: %v", tc.goos, field, tc.path, errors.Join(errs...))
					}
					return
				}
				if len(errs) != 1 || !strings.Contains(errs[0].Error(), field+": ") {
					t.Fatalf("validate(%q) with %s %q = %v, want one %s violation", tc.goos, field, tc.path, errors.Join(errs...), field)
				}
			})
		}
	}
}

func TestValidateRejectsControlCharactersAndInvalidUTF8InEveryStringField(t *testing.T) {
	bad := map[string]string{
		"newline":      "\n",
		"return":       "\r",
		"nul":          "\x00",
		"tab":          "\t",
		"delete":       "\x7f",
		"invalid utf8": "\xff",
	}
	fields := []struct {
		name  string
		label string
		set   func(c *Config, s string)
	}{
		{"Name", "Name: ", func(c *Config, s string) { c.Name += s }},
		{"Description", "Description: ", func(c *Config, s string) { c.Description += s }},
		{"Binary", "Binary: ", func(c *Config, s string) { c.Binary += s }},
		{"Arg", "Args[1]: ", func(c *Config, s string) { c.Args[1] += s }},
		{"Env key", `Env["K`, func(c *Config, s string) { c.Env = map[string]string{"K" + s: "v"} }},
		{"Env value", `Env["K"]: value`, func(c *Config, s string) { c.Env = map[string]string{"K": "v" + s} }},
		{"WorkingDir", "WorkingDir: ", func(c *Config, s string) { c.WorkingDir += s }},
		{"User", "User: ", func(c *Config, s string) { c.User += s }},
	}
	for _, goos := range []string{"linux", "windows"} {
		for _, f := range fields {
			for what, s := range bad {
				t.Run(goos+"/"+f.name+"/"+what, func(t *testing.T) {
					c := validConfig(goos)
					f.set(&c, s)
					errs := violations(t, c.validate(goos))
					if len(errs) != 1 {
						t.Fatalf("got %d violations, want 1: %v", len(errs), errors.Join(errs...))
					}
					msg := errs[0].Error()
					if !strings.Contains(msg, f.label) {
						t.Errorf("violation %q does not name %q", msg, f.label)
					}
					if !strings.Contains(msg, "control character") && !strings.Contains(msg, "invalid UTF-8") {
						t.Errorf("violation %q does not report the control character or invalid UTF-8", msg)
					}
					for _, r := range msg {
						if r < 0x20 || r == 0x7f {
							t.Errorf("violation %q carries control character %U", msg, r)
						}
					}
				})
			}
		}
	}
}

func TestValidateChecksPOSIXUserNames(t *testing.T) {
	tests := []struct {
		user string
		ok   bool
	}{
		{"svc", true},
		{"_x", true},
		{"a-b$", true},
		{"a1_-", true},
		{strings.Repeat("a", 32), true},
		{"Root", false},
		{"1abc", false},
		{"a b", false},
		{strings.Repeat("a", 33), false},
		{"a\nb", false},
		{"$", false},
		{"-a", false},
		{"a$b", false},
		{`NT AUTHORITY\LocalService`, false},
	}
	for _, tc := range tests {
		t.Run(tc.user, func(t *testing.T) {
			c := validConfig("linux")
			c.User = tc.user
			errs := violations(t, c.validate("linux"))
			if tc.ok != (len(errs) == 0) {
				t.Fatalf("User %q: violations %v, want ok=%v", tc.user, errors.Join(errs...), tc.ok)
			}
			if !tc.ok && (len(errs) != 1 || !strings.Contains(errs[0].Error(), "User: ")) {
				t.Fatalf("User %q: violations %v, want one User violation", tc.user, errors.Join(errs...))
			}
		})
	}
}

func TestValidateChecksWindowsAccountNames(t *testing.T) {
	tests := []struct {
		user string
		ok   bool
	}{
		{`NT AUTHORITY\LocalService`, true},
		{`.\svc`, true},
		{`svc`, true},
		{`CORP\svc.user_1-a`, true},
		{"LocalSystem", true},
		{strings.Repeat("a", 104), true},
		{strings.Repeat("a", 105), false},
		{`a\b\c`, false},
		{`CORP\`, false},
		{`\svc`, false},
		{` svc`, false},
		{`svc `, false},
		{`CORP \svc`, false},
		{`CORP\ svc`, false},
		{`svc@corp`, false},
		{`svc%`, false},
	}
	for _, tc := range tests {
		t.Run(tc.user, func(t *testing.T) {
			c := validConfig("windows")
			c.User = tc.user
			errs := violations(t, c.validate("windows"))
			if tc.ok != (len(errs) == 0) {
				t.Fatalf("User %q: violations %v, want ok=%v", tc.user, errors.Join(errs...), tc.ok)
			}
			if !tc.ok && (len(errs) != 1 || !strings.Contains(errs[0].Error(), "User: ")) {
				t.Fatalf("User %q: violations %v, want one User violation", tc.user, errors.Join(errs...))
			}
		})
	}
}

func TestValidateChecksNameEnvKeysAndScope(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(c *Config)
		label string
	}{
		{"missing name", func(c *Config) { c.Name = "" }, "Name: is required"},
		{"missing binary", func(c *Config) { c.Binary = "" }, "Binary: is required"},
		{"name too long", func(c *Config) { c.Name = strings.Repeat("a", 129) }, "Name: "},
		{"name starting with a dot", func(c *Config) { c.Name = ".svc" }, "Name: "},
		{"name starting with a dash", func(c *Config) { c.Name = "-svc" }, "Name: "},
		{"name with a space", func(c *Config) { c.Name = "my svc" }, "Name: "},
		{"name with a slash", func(c *Config) { c.Name = "my/svc" }, "Name: "},
		{"env key starting with a digit", func(c *Config) { c.Env = map[string]string{"1X": "v"} }, `Env["1X"]: key`},
		{"env key with a dash", func(c *Config) { c.Env = map[string]string{"A-B": "v"} }, `Env["A-B"]: key`},
		{"empty env key", func(c *Config) { c.Env = map[string]string{"": "v"} }, `Env[""]: key`},
		{"scope out of range", func(c *Config) { c.Scope = Scope(2) }, "Scope: "},
		{"negative scope", func(c *Config) { c.Scope = Scope(-1) }, "Scope: "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig("linux")
			tc.edit(&c)
			errs := violations(t, c.validate("linux"))
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.label) {
				t.Fatalf("violations %v, want one containing %q", errors.Join(errs...), tc.label)
			}
		})
	}

	c := validConfig("linux")
	c.Name = "A" + strings.Repeat("b.-_9", 25) + "xx"
	if len(c.Name) != 128 {
		t.Fatalf("test name is %d bytes, want 128", len(c.Name))
	}
	if err := c.validate("linux"); err != nil {
		t.Errorf("a 128-byte name is rejected: %v", err)
	}
}

func TestValidateReportsEveryViolation(t *testing.T) {
	c := Config{
		Name:        "",
		Description: "line\nbreak",
		Binary:      "bin/svc",
		Args:        []string{"ok", "\x00", "ok", "\r"},
		Env:         map[string]string{"1A": "v\n", "OK": "fine"},
		WorkingDir:  "var/lib",
		User:        "Root",
		Scope:       Scope(9),
	}
	err := c.validate("linux")
	errs := violations(t, err)
	// Name, Description, Binary, Args[1], Args[3], Env key, Env value,
	// WorkingDir, User, Scope.
	if len(errs) != 10 {
		t.Fatalf("got %d violations, want 10:\n%v", len(errs), err)
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("joined error does not match ErrInvalidConfig")
	}
}

func TestValidateReportsViolationsInAFixedOrder(t *testing.T) {
	c := validConfig("linux")
	c.Args = []string{"ok", "a\nb"}
	c.Env = map[string]string{"Z-KEY": "v", "OK": "v", "1KEY": "v", "M_KEY": "v"}
	const want = "service: invalid config: Args[1]: holds control character U+000A at byte 1\n" +
		`service: invalid config: Env["1KEY"]: key must start with an ASCII letter or '_' and hold only ASCII letters, digits or '_'` + "\n" +
		`service: invalid config: Env["Z-KEY"]: key must start with an ASCII letter or '_' and hold only ASCII letters, digits or '_'`
	for i := range 50 {
		err := c.validate("linux")
		if err == nil {
			t.Fatal("validate accepted the config")
		}
		if got := err.Error(); got != want {
			t.Fatalf("run %d: message\n%s\nwant\n%s", i, got, want)
		}
	}
}

func TestValidateUsesTheRuntimeGOOS(t *testing.T) {
	configs := []Config{
		validConfig(""),
		validConfig("linux"),
		validConfig("windows"),
		{Name: "x", Binary: "rel", User: `.\svc`},
	}
	for i, c := range configs {
		got, rt, empty := c.Validate(), c.validate(runtime.GOOS), c.validate("")
		if errString(got) != errString(rt) || errString(got) != errString(empty) {
			t.Errorf("config %d: Validate() = %v, validate(%q) = %v, validate(\"\") = %v", i, got, runtime.GOOS, rt, empty)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
