package service

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// updateGolden reports whether the -update flag TestMain registers is set.
func updateGolden() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// checkGolden compares got with testdata/<dir>/<name>.golden, which -update
// rewrites. A missing file fails the test.
func checkGolden(t *testing.T, dir, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", dir, name+".golden")
	if updateGolden() {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the golden file (run with -update to create it): %v", err)
	}
	if string(want) != string(got) {
		t.Errorf("output differs from %s\n got: %q\nwant: %q", path, got, want)
	}
}

// escapingConfig carries a space, '%', '$', '\' and '"' in every escaped
// field, and an empty argument.
func escapingConfig() Config {
	return Config{
		Name:        "esc",
		Description: `100% "odd" $HOME \n`,
		Binary:      `/opt/my app/bin%$\"x`,
		Args:        []string{"--name=a b", "", `50%`, `$HOME`, `back\slash`, `say "hi"`},
		Env:         map[string]string{"ODD": `a b%c$d\e"f`},
		WorkingDir:  `/srv/my dir%$\"w`,
	}
}

func TestRenderUnitGolden(t *testing.T) {
	strict := Hardening{
		NoNewPrivileges:       true,
		PrivateTmp:            true,
		ProtectSystem:         "strict",
		ProtectHome:           "tmpfs",
		ProtectKernelTunables: true,
		ProtectControlGroups:  true,
		RestrictSUIDSGID:      true,
		ReadWritePaths:        []string{"/var/lib/demo", "/var/log/demo"},
	}
	escHardening := DefaultHardening(ScopeSystem)
	escHardening.ReadWritePaths = []string{`/data/a b%$\"p`}

	tests := []struct {
		name string
		cfg  Config
		h    Hardening
		n    notify
	}{
		{
			name: "system-default",
			cfg:  Config{Name: "demo", Description: "Demo service", Binary: "/usr/bin/demo"},
			h:    DefaultHardening(ScopeSystem),
		},
		{
			name: "system-user-env",
			cfg: Config{
				Name:        "demo",
				Description: "Demo service",
				Binary:      "/usr/bin/demo",
				Args:        []string{"serve", "--port=8080"},
				Env:         map[string]string{"LOG_LEVEL": "debug", "APP_MODE": "prod", "_FEATURE": "on"},
				User:        "demo",
			},
			h: DefaultHardening(ScopeSystem),
		},
		{
			name: "user-default",
			cfg: Config{
				Name:        "demo",
				Description: "Demo service",
				Binary:      "/home/me/bin/demo",
				Args:        []string{"serve"},
				User:        "ignored",
				Scope:       ScopeUser,
			},
			h: DefaultHardening(ScopeUser),
		},
		{
			name: "escaping",
			cfg:  escapingConfig(),
			h:    escHardening,
		},
		{
			name: "trailing-backslash",
			cfg: Config{
				Name:        "demo",
				Description: `Demo service\\\`,
				Binary:      "/usr/bin/demo",
				WorkingDir:  `/srv/app\`,
				User:        "demo",
			},
			h: DefaultHardening(ScopeSystem),
		},
		{
			name: "hardening-strict-with-paths",
			cfg: Config{
				Name:       "demo",
				Binary:     "/usr/bin/demo",
				WorkingDir: "/var/lib/demo/work",
			},
			h: strict,
		},
		{
			name: "hardening-zero",
			cfg: Config{
				Name:       "demo",
				Binary:     "/usr/bin/demo",
				WorkingDir: "/var/lib/demo",
			},
			h: Hardening{},
		},
		{
			name: "notify",
			cfg:  Config{Name: "demo", Description: "Demo service", Binary: "/usr/bin/demo"},
			h:    DefaultHardening(ScopeSystem),
			n:    notify{enabled: true},
		},
		{
			name: "notify-watchdog",
			cfg:  Config{Name: "demo", Description: "Demo service", Binary: "/usr/bin/demo"},
			h:    DefaultHardening(ScopeSystem),
			n:    notify{enabled: true, watchdog: 30 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit := renderUnit(tt.cfg, tt.h, tt.n)
			checkGolden(t, "systemd", tt.name, unit)
			checkNoContinuation(t, unit)
		})
	}
}

// checkNoContinuation fails the test for every line of unit that ends in an
// odd run of '\', which systemd joins with the line after it, so that the
// next directive becomes part of the value.
func checkNoContinuation(t *testing.T, unit []byte) {
	t.Helper()
	for i, l := range strings.Split(string(unit), "\n") {
		run := len(l) - len(strings.TrimRight(l, `\`))
		if run%2 == 1 {
			t.Errorf("line %d %q ends in an unescaped '\\' and continues onto the next line", i+1, l)
		}
	}
}

func TestRenderUnitTrailingBackslashEndsTheLine(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "working-dir",
			cfg:  Config{Name: "demo", Binary: "/usr/bin/demo", WorkingDir: `/srv/app\`, User: "demo"},
			want: []string{`WorkingDirectory=/srv/app\/`, "User=demo"},
		},
		{
			name: "description",
			cfg:  Config{Name: "demo", Description: `Demo\`, Binary: "/usr/bin/demo"},
			want: []string{"Description=Demo", "After=network-online.target"},
		},
		{
			name: "description-only-backslashes",
			cfg:  Config{Name: "demo", Description: `\\\`, Binary: "/usr/bin/demo"},
			want: []string{"[Unit]", "After=network-online.target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit := renderUnit(tt.cfg, Hardening{}, notify{})
			checkNoContinuation(t, unit)
			if !strings.Contains(string(unit), strings.Join(tt.want, "\n")+"\n") {
				t.Errorf("unit does not hold the consecutive lines %q:\n%s", tt.want, unit)
			}
		})
	}
}

func TestRenderUnitReadWritePathsQuotesTrailingBackslash(t *testing.T) {
	cfg := Config{Name: "demo", Binary: "/usr/bin/demo", WorkingDir: `/srv/app\`}
	unit := renderUnit(cfg, Hardening{ProtectSystem: "strict"}, notify{})
	want := `ReadWritePaths="/srv/app\\"`
	if got := unitLines(unit, "ReadWritePaths"); len(got) != 1 || got[0] != want {
		t.Errorf("ReadWritePaths lines = %q, want [%q]", got, want)
	}
}

// unitLines returns the lines of unit that start with key followed by '='.
func unitLines(unit []byte, key string) []string {
	var out []string
	for l := range strings.SplitSeq(string(unit), "\n") {
		if strings.HasPrefix(l, key+"=") {
			out = append(out, l)
		}
	}
	return out
}

func TestRenderUnitDollarIsDoubledOnlyInExecStart(t *testing.T) {
	unit := renderUnit(escapingConfig(), Hardening{}, notify{})

	env := unitLines(unit, "Environment")
	if len(env) != 1 {
		t.Fatalf("Environment lines = %q, want one", env)
	}
	if want := `Environment="ODD=a b%%c$d\\e\"f"`; env[0] != want {
		t.Errorf("Environment line = %q, want %q", env[0], want)
	}
	if strings.Contains(env[0], "$$") {
		t.Errorf("Environment line %q doubles '$'; systemd expands no variables there", env[0])
	}

	exec := unitLines(unit, "ExecStart")
	if len(exec) != 1 {
		t.Fatalf("ExecStart lines = %q, want one", exec)
	}
	if !strings.Contains(exec[0], `"$$HOME"`) {
		t.Errorf("ExecStart line %q does not hold the argument $HOME as \"$$HOME\"", exec[0])
	}
	if !strings.Contains(exec[0], ` "" `) {
		t.Errorf("ExecStart line %q does not render the empty argument as \"\"", exec[0])
	}
}

func TestRenderUnitKeepsCallerReadWritePaths(t *testing.T) {
	paths := make([]string, 1, 2)
	paths[0] = "/var/lib/demo"
	h := Hardening{ProtectSystem: "strict", ReadWritePaths: paths}
	cfg := Config{Name: "demo", Binary: "/usr/bin/demo", WorkingDir: "/srv/demo"}

	unit := renderUnit(cfg, h, notify{})

	if got := paths[:cap(paths)][1]; got != "" {
		t.Errorf("renderUnit wrote %q into the caller's ReadWritePaths backing array", got)
	}
	want := []string{`ReadWritePaths="/var/lib/demo" "/srv/demo"`}
	if got := unitLines(unit, "ReadWritePaths"); strings.Join(got, "\n") != want[0] {
		t.Errorf("ReadWritePaths lines = %q, want %q", got, want)
	}
}

func TestRenderUnitWorkingDirWritableOnlyUnderProtection(t *testing.T) {
	cfg := Config{Name: "demo", Binary: "/usr/bin/demo", WorkingDir: "/srv/demo"}
	tests := []struct {
		name string
		h    Hardening
		want int
	}{
		{name: "none", h: Hardening{}, want: 0},
		{name: "protect-system", h: Hardening{ProtectSystem: "full"}, want: 1},
		{name: "protect-home", h: Hardening{ProtectHome: "read-only"}, want: 1},
		{name: "already-listed", h: Hardening{ProtectSystem: "strict", ReadWritePaths: []string{"/srv/demo"}}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := unitLines(renderUnit(cfg, tt.h, notify{}), "ReadWritePaths")
			if got := strings.Count(strings.Join(lines, "\n"), `"/srv/demo"`); got != tt.want {
				t.Errorf("\"/srv/demo\" appears %d times in ReadWritePaths %q, want %d", got, lines, tt.want)
			}
		})
	}
}

func TestRenderUnitWatchdogIsWholeMilliseconds(t *testing.T) {
	cfg := Config{Name: "demo", Binary: "/usr/bin/demo"}
	tests := []struct {
		name string
		n    notify
		want []string
	}{
		{name: "simple", n: notify{watchdog: time.Second}, want: nil},
		{name: "no-watchdog", n: notify{enabled: true}, want: nil},
		{name: "sub-second", n: notify{enabled: true, watchdog: 1500 * time.Millisecond}, want: []string{"WatchdogSec=1500ms"}},
		{name: "one-millisecond", n: notify{enabled: true, watchdog: time.Millisecond}, want: []string{"WatchdogSec=1ms"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unitLines(renderUnit(cfg, Hardening{}, tt.n), "WatchdogSec")
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("WatchdogSec lines = %q, want %q", got, tt.want)
			}
		})
	}
}
