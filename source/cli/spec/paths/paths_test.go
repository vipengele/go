package paths_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/commons/validate"
)

const (
	app      = "agentic-toolkit"
	homeVar  = "ATK_HOME"
	fakeHome = "/home/u"
)

func identity() spec.Identity {
	return spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   app,
	}
}

// lookup returns a LookupEnv reading env, in which a key mapped to "" is set to the empty string.
func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func homeDir(home string) func() (string, error) {
	return func() (string, error) { return home, nil }
}

// noHomeDir is a UserHomeDir that fails the test when it is called.
func noHomeDir(t *testing.T) func() (string, error) {
	t.Helper()
	return func() (string, error) {
		t.Error("UserHomeDir called, want the home directory not looked up")
		return "", errors.New("no home")
	}
}

type want struct {
	config, state, cache, data, log string
}

func dirsOf(d paths.Dirs) want {
	return want{config: d.Config(), state: d.State(), cache: d.Cache(), data: d.Data(), log: d.Log()}
}

func j(elem ...string) string {
	return filepath.Join(elem...)
}

// under returns the five kinds as subdirectories of a home root.
func under(root string) want {
	return want{
		config: j(root, "config"),
		state:  j(root, "state"),
		cache:  j(root, "cache"),
		data:   j(root, "data"),
		log:    j(root, "log"),
	}
}

func explicitOptions() paths.Options {
	return paths.Options{
		ConfigDir: "/x/config",
		StateDir:  "/x/state",
		CacheDir:  "/x/cache",
		DataDir:   "/x/data",
		LogDir:    "/x/log",
	}
}

func explicitWant() want {
	return want{config: "/x/config", state: "/x/state", cache: "/x/cache", data: "/x/data", log: "/x/log"}
}

func linuxDefaults() want {
	return want{
		config: j(fakeHome, ".config", app),
		state:  j(fakeHome, ".local", "state", app),
		cache:  j(fakeHome, ".cache", app),
		data:   j(fakeHome, ".local", "share", app),
		log:    j(fakeHome, ".local", "state", app, "log"),
	}
}

func darwinDefaults() want {
	config := j(fakeHome, "Library", "Application Support", app)
	return want{
		config: config,
		state:  j(config, "state"),
		cache:  j(fakeHome, "Library", "Caches", app),
		data:   j(config, "data"),
		log:    j(fakeHome, "Library", "Logs", app),
	}
}

func windowsLayout(roaming, local string) want {
	return want{
		config: j(roaming, app),
		state:  j(local, app, "state"),
		cache:  j(local, app, "cache"),
		data:   j(local, app, "data"),
		log:    j(local, app, "log"),
	}
}

func xdgEnv() map[string]string {
	return map[string]string{
		"XDG_CONFIG_HOME": "/xdg/config",
		"XDG_STATE_HOME":  "/xdg/state",
		"XDG_CACHE_HOME":  "/xdg/cache",
		"XDG_DATA_HOME":   "/xdg/data",
	}
}

func windowsEnv() map[string]string {
	return map[string]string{"AppData": "/w/roaming", "LocalAppData": "/w/local"}
}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func TestNewLayouts(t *testing.T) {
	tests := []struct {
		name string
		goos string
		env  map[string]string
		opts paths.Options
		want want
	}{
		// Linux.
		{"linux defaults", "linux", nil, paths.Options{}, linuxDefaults()},
		{"linux XDG base directories take DirName appended", "linux", xdgEnv(), paths.Options{}, want{
			config: j("/xdg/config", app),
			state:  j("/xdg/state", app),
			cache:  j("/xdg/cache", app),
			data:   j("/xdg/data", app),
			log:    j("/xdg/state", app, "log"),
		}},
		{"linux <PREFIX>_HOME beats XDG", "linux",
			merge(xdgEnv(), map[string]string{homeVar: "/env/home"}), paths.Options{}, under("/env/home")},
		{"linux Options.Home beats <PREFIX>_HOME", "linux",
			merge(xdgEnv(), map[string]string{homeVar: "/env/home"}), paths.Options{Home: "/opt/home"}, under("/opt/home")},
		{"linux explicit options beat Options.Home", "linux",
			merge(xdgEnv(), map[string]string{homeVar: "/env/home"}),
			func() paths.Options { o := explicitOptions(); o.Home = "/opt/home"; return o }(), explicitWant()},
		{"linux one explicit option leaves the rest to Options.Home", "linux", nil,
			paths.Options{Home: "/opt/home", CacheDir: "/x/cache"},
			func() want { w := under("/opt/home"); w.cache = "/x/cache"; return w }()},
		{"linux log follows an explicit state directory", "linux", nil, paths.Options{StateDir: "/x/state"},
			func() want { w := linuxDefaults(); w.state = "/x/state"; w.log = j("/x/state", "log"); return w }()},
		{"linux explicit log directory beats the derivation", "linux", nil,
			paths.Options{StateDir: "/x/state", LogDir: "/x/log"},
			func() want { w := linuxDefaults(); w.state = "/x/state"; w.log = "/x/log"; return w }()},
		{"linux relative XDG base directory is ignored", "linux",
			map[string]string{"XDG_CONFIG_HOME": "relative/config"}, paths.Options{}, linuxDefaults()},
		{"other GOOS takes the XDG layout", "freebsd", xdgEnv(), paths.Options{}, want{
			config: j("/xdg/config", app),
			state:  j("/xdg/state", app),
			cache:  j("/xdg/cache", app),
			data:   j("/xdg/data", app),
			log:    j("/xdg/state", app, "log"),
		}},
		{"other GOOS defaults", "openbsd", nil, paths.Options{}, linuxDefaults()},

		// macOS.
		{"darwin defaults", "darwin", nil, paths.Options{}, darwinDefaults()},
		{"darwin ignores XDG", "darwin", xdgEnv(), paths.Options{}, darwinDefaults()},
		{"darwin <PREFIX>_HOME beats the default", "darwin",
			map[string]string{homeVar: "/env/home"}, paths.Options{}, under("/env/home")},
		{"darwin Options.Home beats <PREFIX>_HOME", "darwin",
			map[string]string{homeVar: "/env/home"}, paths.Options{Home: "/opt/home"}, under("/opt/home")},
		{"darwin explicit options beat Options.Home", "darwin", nil,
			func() paths.Options { o := explicitOptions(); o.Home = "/opt/home"; return o }(), explicitWant()},
		{"darwin state and data follow an explicit config directory", "darwin", nil,
			paths.Options{ConfigDir: "/x/config"},
			func() want {
				w := darwinDefaults()
				w.config, w.state, w.data = "/x/config", j("/x/config", "state"), j("/x/config", "data")
				return w
			}()},
		{"darwin explicit state and data beat the derivation", "darwin", nil,
			paths.Options{ConfigDir: "/x/config", StateDir: "/x/state", DataDir: "/x/data"},
			func() want {
				w := darwinDefaults()
				w.config, w.state, w.data = "/x/config", "/x/state", "/x/data"
				return w
			}()},

		// Windows.
		{"windows defaults", "windows", windowsEnv(), paths.Options{}, windowsLayout("/w/roaming", "/w/local")},
		{"windows ignores XDG", "windows", merge(windowsEnv(), xdgEnv()), paths.Options{},
			windowsLayout("/w/roaming", "/w/local")},
		{"windows falls back to the home directory without AppData and LocalAppData", "windows", nil,
			paths.Options{}, windowsLayout(j(fakeHome, "AppData", "Roaming"), j(fakeHome, "AppData", "Local"))},
		{"windows <PREFIX>_HOME beats the default", "windows",
			merge(windowsEnv(), map[string]string{homeVar: "/env/home"}), paths.Options{}, under("/env/home")},
		{"windows Options.Home beats <PREFIX>_HOME", "windows",
			merge(windowsEnv(), map[string]string{homeVar: "/env/home"}), paths.Options{Home: "/opt/home"},
			under("/opt/home")},
		{"windows explicit options beat Options.Home", "windows", windowsEnv(),
			func() paths.Options { o := explicitOptions(); o.Home = "/opt/home"; return o }(), explicitWant()},
		{"windows explicit option beats the default", "windows", windowsEnv(), paths.Options{LogDir: "/x/log"},
			func() want { w := windowsLayout("/w/roaming", "/w/local"); w.log = "/x/log"; return w }()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.GOOS = tc.goos
			opts.LookupEnv = lookup(tc.env)
			opts.UserHomeDir = homeDir(fakeHome)
			d, err := paths.New(identity(), opts)
			if err != nil {
				t.Fatalf("New() = %v, want nil", err)
			}
			if got := dirsOf(d); got != tc.want {
				t.Errorf("New() =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func TestNewEmptyVariableCountsAsUnset(t *testing.T) {
	empty := map[string]string{
		homeVar:           "",
		"XDG_CONFIG_HOME": "",
		"XDG_STATE_HOME":  "",
		"XDG_CACHE_HOME":  "",
		"XDG_DATA_HOME":   "",
		"AppData":         "",
		"LocalAppData":    "",
	}
	tests := []struct {
		goos string
		want want
	}{
		{"linux", linuxDefaults()},
		{"darwin", darwinDefaults()},
		{"windows", windowsLayout(j(fakeHome, "AppData", "Roaming"), j(fakeHome, "AppData", "Local"))},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			d, err := paths.New(identity(), paths.Options{
				GOOS:        tc.goos,
				LookupEnv:   lookup(empty),
				UserHomeDir: homeDir(fakeHome),
			})
			if err != nil {
				t.Fatalf("New() = %v, want nil", err)
			}
			if got := dirsOf(d); got != tc.want {
				t.Errorf("New() =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func TestNewDefaultsAreDistinct(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd"} {
		t.Run(goos, func(t *testing.T) {
			d, err := paths.New(identity(), paths.Options{
				GOOS:        goos,
				LookupEnv:   lookup(nil),
				UserHomeDir: homeDir(fakeHome),
			})
			if err != nil {
				t.Fatalf("New() = %v, want nil", err)
			}
			seen := map[string]bool{}
			for _, dir := range []string{d.Config(), d.State(), d.Cache(), d.Data(), d.Log()} {
				if seen[dir] {
					t.Errorf("two kinds share the directory %q", dir)
				}
				seen[dir] = true
			}
		})
	}
}

func TestNewDoesNotRejectExplicitCollisions(t *testing.T) {
	d, err := paths.New(identity(), paths.Options{
		GOOS:        "linux",
		StateDir:    "/x/same",
		LogDir:      "/x/same",
		LookupEnv:   lookup(nil),
		UserHomeDir: homeDir(fakeHome),
	})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if d.State() != "/x/same" || d.Log() != "/x/same" {
		t.Errorf("State() = %q, Log() = %q, want both /x/same", d.State(), d.Log())
	}
}

func TestNewMissingHomeDirectory(t *testing.T) {
	cause := errors.New("no home")
	tests := []struct {
		name        string
		goos        string
		env         map[string]string
		opts        paths.Options
		userHomeDir func() (string, error)
	}{
		{"linux defaults", "linux", nil, paths.Options{}, func() (string, error) { return "", cause }},
		{"darwin defaults", "darwin", nil, paths.Options{}, func() (string, error) { return "", cause }},
		{"windows without AppData", "windows", nil, paths.Options{}, func() (string, error) { return "", cause }},
		{"windows without LocalAppData", "windows", map[string]string{"AppData": "/w/roaming"}, paths.Options{},
			func() (string, error) { return "", cause }},
		{"linux with one kind left to the default", "linux", nil,
			paths.Options{ConfigDir: "/x/config", StateDir: "/x/state", DataDir: "/x/data"},
			func() (string, error) { return "", cause }},
		{"empty home directory", "linux", nil, paths.Options{}, func() (string, error) { return "", nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.GOOS = tc.goos
			opts.LookupEnv = lookup(tc.env)
			opts.UserHomeDir = tc.userHomeDir
			d, err := paths.New(identity(), opts)
			if !errors.Is(err, paths.ErrNoHomeDir) {
				t.Fatalf("New() = %v, want ErrNoHomeDir", err)
			}
			if d != (paths.Dirs{}) {
				t.Errorf("New() returned %+v with its error, want the zero Dirs", dirsOf(d))
			}
		})
	}
}

func TestNewMissingHomeDirectoryWrapsCause(t *testing.T) {
	cause := errors.New("no home")
	_, err := paths.New(identity(), paths.Options{
		GOOS:        "linux",
		LookupEnv:   lookup(nil),
		UserHomeDir: func() (string, error) { return "", cause },
	})
	if !errors.Is(err, cause) {
		t.Fatalf("New() = %v, want it to wrap the UserHomeDir error", err)
	}
}

func TestNewLooksUpHomeDirectoryOnlyWhenNeeded(t *testing.T) {
	tests := []struct {
		name string
		goos string
		env  map[string]string
		opts paths.Options
	}{
		{"Options.Home", "linux", nil, paths.Options{Home: "/opt/home"}},
		{"<PREFIX>_HOME", "darwin", map[string]string{homeVar: "/env/home"}, paths.Options{}},
		{"every kind explicit", "darwin", nil, explicitOptions()},
		{"every XDG base directory set", "linux", xdgEnv(), paths.Options{}},
		{"AppData and LocalAppData set", "windows", windowsEnv(), paths.Options{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.GOOS = tc.goos
			opts.LookupEnv = lookup(tc.env)
			opts.UserHomeDir = noHomeDir(t)
			if _, err := paths.New(identity(), opts); err != nil {
				t.Fatalf("New() = %v, want nil", err)
			}
		})
	}
}

func TestNewLooksUpHomeDirectoryOnce(t *testing.T) {
	calls := 0
	_, err := paths.New(identity(), paths.Options{
		GOOS:      "darwin",
		LookupEnv: lookup(nil),
		UserHomeDir: func() (string, error) {
			calls++
			return fakeHome, nil
		},
	})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("UserHomeDir called %d times, want 1", calls)
	}
}

func TestNewRejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name string
		set  func(*spec.Identity)
		want error
	}{
		{"zero Identity", func(i *spec.Identity) { *i = spec.Identity{} }, validate.ErrEmpty},
		{"empty DirName", func(i *spec.Identity) { i.DirName = "" }, validate.ErrEmpty},
		{"DirName with a slash", func(i *spec.Identity) { i.DirName = "a/b" }, validate.ErrInvalid},
		{"lowercase EnvPrefix", func(i *spec.Identity) { i.EnvPrefix = "atk" }, validate.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := identity()
			tc.set(&id)
			_, err := paths.New(id, paths.Options{
				GOOS:        "linux",
				Home:        "/opt/home",
				LookupEnv:   lookup(nil),
				UserHomeDir: noHomeDir(t),
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("New() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewEmptyGOOSMeansRuntimeGOOS(t *testing.T) {
	opts := paths.Options{LookupEnv: lookup(windowsEnv()), UserHomeDir: homeDir(fakeHome)}
	got, err := paths.New(identity(), opts)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	opts.GOOS = runtime.GOOS
	want, err := paths.New(identity(), opts)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if got != want {
		t.Errorf("New() with GOOS \"\" =\n  %+v\nwant, as with GOOS %q,\n  %+v", dirsOf(got), runtime.GOOS, dirsOf(want))
	}
}

func TestNewNilSeamsReadTheProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(homeVar, "")
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		t.Setenv(v, "")
	}
	d, err := paths.New(identity(), paths.Options{GOOS: "linux"})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if want := j(home, ".config", app); d.Config() != want {
		t.Errorf("Config() = %q, want %q", d.Config(), want)
	}

	t.Setenv(homeVar, "/env/home")
	d, err = paths.New(identity(), paths.Options{GOOS: "linux"})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if got := dirsOf(d); got != under("/env/home") {
		t.Errorf("New() =\n  %+v\nwant\n  %+v", got, under("/env/home"))
	}
}

func TestNewTouchesNoFileSystem(t *testing.T) {
	root := j(t.TempDir(), "absent")
	d, err := paths.New(identity(), paths.Options{GOOS: "linux", Home: root, LookupEnv: lookup(nil)})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if d.Config() != j(root, "config") {
		t.Errorf("Config() = %q, want %q", d.Config(), j(root, "config"))
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%q) = %v, want it not to exist", root, err)
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		kind paths.Kind
		want string
	}{
		{paths.Config, "config"},
		{paths.State, "state"},
		{paths.Cache, "cache"},
		{paths.Data, "data"},
		{paths.Log, "log"},
		{paths.Kind(0), "Kind(0)"},
		{paths.Kind(6), "Kind(6)"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

func TestZeroDirsHoldsNoDirectory(t *testing.T) {
	if got := dirsOf(paths.Dirs{}); got != (want{}) {
		t.Errorf("zero Dirs = %+v, want every directory empty", got)
	}
}
