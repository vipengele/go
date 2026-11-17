package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type loadServer struct {
	Host       string
	Port       int
	MaxRetries int
	Timeout    time.Duration
}

type loadConfig struct {
	Name   string
	Debug  bool
	Server loadServer
	Tags   []string
}

func loadDefaults() loadConfig {
	return loadConfig{
		Name:   "app",
		Server: loadServer{Host: "localhost", Port: 80, MaxRetries: 1, Timeout: 30 * time.Second},
		Tags:   []string{"base"},
	}
}

// writeFile writes content to a file named name in a temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// stubProvider returns fixed entries or a fixed error.
type stubProvider struct {
	entries []Entry
	err     error
}

func (p *stubProvider) Load(*Schema) ([]Entry, error) { return p.entries, p.err }
func (p *stubProvider) Strict() bool                  { return false }

func TestLoadPrecedence(t *testing.T) {
	path := writeFile(t, "config.yaml", "name: from-file\nserver:\n  host: file-host\n  port: 8080\n  max_retries: 2\ntags: [x, y]\n")
	env := mapLookup(map[string]string{
		"APP_SERVER_PORT":        "9090",
		"APP_SERVER_MAX_RETRIES": "3",
	})
	flags := map[string]string{"server.max-retries": "4"}

	res, err := Load(loadDefaults(), File(path), Env("APP", env), Flags(flags))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := loadConfig{
		Name:   "from-file",
		Server: loadServer{Host: "file-host", Port: 9090, MaxRetries: 4, Timeout: 30 * time.Second},
		Tags:   []string{"x", "y"},
	}
	if got := res.Value(); !reflect.DeepEqual(got, want) {
		t.Errorf("Value() = %+v, want %+v", got, want)
	}
	wantSources := []Source{
		{Key: "name", Value: "from-file", Origin: "config.yaml:1"},
		{Key: "debug", Value: "false", Origin: "default"},
		{Key: "server.host", Value: "file-host", Origin: "config.yaml:3"},
		{Key: "server.port", Value: "9090", Origin: "env APP_SERVER_PORT"},
		{Key: "server.max_retries", Value: "4", Origin: "flag --server.max-retries"},
		{Key: "server.timeout", Value: "30s", Origin: "default"},
		{Key: "tags", Value: "x,y", Origin: "config.yaml:6"},
	}
	if got := res.Sources(); !reflect.DeepEqual(got, wantSources) {
		t.Errorf("Sources() =\n%+v\nwant\n%+v", got, wantSources)
	}
}

func TestLoadOrderIsTheCallers(t *testing.T) {
	low := &stubProvider{entries: []Entry{{Key: "name", Value: "low", Origin: "low"}}}
	high := &stubProvider{entries: []Entry{{Key: "name", Value: "high", Origin: "high"}}}
	tests := []struct {
		name      string
		providers []Provider
		want      string
	}{
		{name: "no providers", want: "app"},
		{name: "later overrides earlier", providers: []Provider{low, high}, want: "high"},
		{name: "reversed", providers: []Provider{high, low}, want: "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Load(loadDefaults(), tt.providers...)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := res.Value().Name; got != tt.want {
				t.Errorf("Name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadUntouchedIsDefault(t *testing.T) {
	res, err := Load(loadDefaults(), Env("APP", mapLookup(map[string]string{})), Flags(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := res.Value(), loadDefaults(); !reflect.DeepEqual(got, want) {
		t.Errorf("Value() = %+v, want %+v", got, want)
	}
	for _, s := range res.Sources() {
		if s.Origin != "default" {
			t.Errorf("%s: origin %q, want default", s.Key, s.Origin)
		}
	}
}

func TestLoadDoesNotChangeDefaults(t *testing.T) {
	defaults := loadDefaults()
	_, err := Load(defaults, Env("APP", mapLookup(map[string]string{"APP_TAGS": "a,b"})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := loadDefaults(); !reflect.DeepEqual(defaults, want) {
		t.Errorf("defaults = %+v, want %+v", defaults, want)
	}
}

func TestLoadValueSharesNoSliceWithDefaults(t *testing.T) {
	type sliceConfig struct {
		Tags  []string
		Ports []int
		Empty []string
		None  []string
	}
	newDefaults := func() sliceConfig {
		return sliceConfig{Tags: []string{"a", "b"}, Ports: []int{80}, Empty: []string{}}
	}
	change := func(c *sliceConfig) {
		c.Tags[0] = "changed"
		c.Ports[0] = 1
	}
	tests := []struct {
		name string
		// run changes one side and returns the other, which must still equal newDefaults().
		run func(defaults *sliceConfig, res Result[sliceConfig]) sliceConfig
	}{
		{
			name: "changing the value leaves defaults",
			run: func(defaults *sliceConfig, res Result[sliceConfig]) sliceConfig {
				value := res.Value()
				change(&value)
				return *defaults
			},
		},
		{
			name: "changing defaults leaves the value",
			run: func(defaults *sliceConfig, res Result[sliceConfig]) sliceConfig {
				change(defaults)
				return res.Value()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults := newDefaults()
			res, err := Load(defaults)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tt.run(&defaults, res); !reflect.DeepEqual(got, newDefaults()) {
				t.Errorf("got %+v, want %+v", got, newDefaults())
			}
		})
	}
}

func TestLoadKeepsNilAndEmptySlicesDistinct(t *testing.T) {
	type sliceConfig struct {
		Empty []string
		None  []string
	}
	res, err := Load(sliceConfig{Empty: []string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	value := res.Value()
	if value.None != nil {
		t.Errorf("None = %#v, want nil", value.None)
	}
	if value.Empty == nil {
		t.Error("Empty = nil, want an empty slice")
	}
}

func TestLoadErrors(t *testing.T) {
	bad := writeFile(t, "config.yaml", "server:\n  port: abc\n  bogus: 1\n")
	good := writeFile(t, "good.yaml", "server:\n  port: abc\n")
	tests := []struct {
		name      string
		providers []Provider
		want      []string
	}{
		{
			name:      "conversion names key, text and origin",
			providers: []Provider{Env("APP", mapLookup(map[string]string{"APP_SERVER_MAX_RETRIES": "abc"}))},
			want:      []string{`server.max_retries: invalid int "abc" from env APP_SERVER_MAX_RETRIES`},
		},
		{
			name:      "nil provider",
			providers: []Provider{nil},
			want:      []string{"config: provider 0 is nil"},
		},
		{
			name:      "typed nil provider",
			providers: []Provider{Flags(nil), (*FileProvider)(nil)},
			want:      []string{"config: provider 1 is nil"},
		},
		{
			name: "every problem of every provider in layer order",
			providers: []Provider{
				File(bad),
				Env("APP", mapLookup(map[string]string{"APP_SERVER_PORT": "x", "APP_DEBUG": "maybe"})),
				Flags(map[string]string{"nope": "1"}),
				nil,
				File(good),
				Flags(map[string]string{"server.timeout": "soon"}),
			},
			want: []string{
				`config.yaml:3: unknown key "server.bogus"`,
				"flag --nope: unknown key",
				"config: provider 3 is nil",
				`debug: invalid bool "maybe" from env APP_DEBUG`,
				`server.port: invalid int "x" from env APP_SERVER_PORT`,
				`server.port: invalid int "abc" from good.yaml:2`,
				`server.timeout: invalid duration "soon" from flag --server.timeout`,
			},
		},
		{
			name:      "unknown key from a provider",
			providers: []Provider{&stubProvider{entries: []Entry{{Key: "missing", Value: "1", Origin: "stub"}}}},
			want:      []string{"missing: unknown key from stub"},
		},
		{
			name:      "list for a single value",
			providers: []Provider{&stubProvider{entries: []Entry{{Key: "name", Items: []string{"a"}, List: true, Origin: "stub"}}}},
			want:      []string{"name: a list from stub, but it holds a single string"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Load(loadDefaults(), tt.providers...)
			if err == nil {
				t.Fatal("Load: no error")
			}
			if got, want := err.Error(), strings.Join(tt.want, "\n"); got != want {
				t.Errorf("error =\n%s\nwant\n%s", got, want)
			}
			if !reflect.DeepEqual(res, Result[loadConfig]{}) {
				t.Errorf("Result = %+v, want the zero Result", res)
			}
		})
	}
}

func TestLoadProviderErrorIsKept(t *testing.T) {
	sentinel := errors.New("stub failed")
	_, err := Load(loadDefaults(), &stubProvider{
		entries: []Entry{{Key: "server.port", Value: "abc", Origin: "stub"}},
		err:     sentinel,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	if strings.Contains(err.Error(), "server.port") {
		t.Errorf("error = %v; a provider whose Load fails sets nothing", err)
	}
}

func TestLoadSchemaErrors(t *testing.T) {
	type badField struct {
		P *int
	}
	tests := []struct {
		name string
		load func() error
		want string
	}{
		{
			name: "pointer",
			load: func() error { _, err := Load(&loadConfig{}); return err },
			want: "configuration type is *config.loadConfig, not a struct",
		},
		{
			name: "not a struct",
			load: func() error { _, err := Load(3); return err },
			want: "configuration type is int, not a struct",
		},
		{
			name: "interface",
			load: func() error { _, err := Load[any](loadConfig{}); return err },
			want: "configuration type is interface {}, not a struct",
		},
		{
			name: "invalid schema",
			load: func() error { _, err := Load(badField{}); return err },
			want: "field badField.P: pointer fields are not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.load()
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("error = %v, want ErrSchema", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

type mergeMeta struct {
	Owner string
}

type mergeHost struct {
	Addr string
	Port int
}

type mergeEnvName string

type mergeProfile struct {
	Name  string
	Meta  mergeMeta
	Env   map[mergeEnvName]string
	Hosts []mergeHost
	Tags  []string
}

type mergeConfig struct {
	Name     string
	Profiles []mergeProfile `merge:"name"`
	Labels   map[string]string
	Services map[string]mergeHost `merge:"keys"`
	Steps    []mergeHost
	Matrix   map[string]map[string]int
}

func mergeDefaults() mergeConfig {
	return mergeConfig{
		Name: "app",
		Profiles: []mergeProfile{
			{Name: "dev", Env: map[mergeEnvName]string{"FOO": "1"}, Tags: []string{"a"}},
			{Name: "prod", Hosts: []mergeHost{{Addr: "p", Port: 1}}},
		},
		Labels:   map[string]string{"x": "1", "y": "2"},
		Services: map[string]mergeHost{"web": {Addr: "w", Port: 80}, "db": {Addr: "d", Port: 5432}},
		Steps:    []mergeHost{{Addr: "a", Port: 1}, {Addr: "b", Port: 2}},
		Matrix:   map[string]map[string]int{"m": {"n": 1}},
	}
}

func TestLoadCollectionReplacesWithoutMergeTag(t *testing.T) {
	tests := []struct {
		name   string
		layers []string
		want   func(c *mergeConfig)
	}{
		{
			name:   "list replaced whole",
			layers: []string{"steps:\n  - addr: c\n"},
			want:   func(c *mergeConfig) { c.Steps = []mergeHost{{Addr: "c"}} },
		},
		{
			name:   "map replaced whole",
			layers: []string{"labels:\n  z: \"3\"\n"},
			want:   func(c *mergeConfig) { c.Labels = map[string]string{"z": "3"} },
		},
		{
			name:   "nested map replaced whole",
			layers: []string{"matrix:\n  o:\n    p: 2\n"},
			want:   func(c *mergeConfig) { c.Matrix = map[string]map[string]int{"o": {"p": 2}} },
		},
		{
			name:   "later layer replaces earlier layer",
			layers: []string{"steps:\n  - addr: c\nlabels:\n  z: \"3\"\n", "steps:\n  - addr: d\n    port: 4\nlabels:\n  w: \"5\"\n"},
			want: func(c *mergeConfig) {
				c.Steps = []mergeHost{{Addr: "d", Port: 4}}
				c.Labels = map[string]string{"w": "5"}
			},
		},
		{
			name:   "empty later collections replace",
			layers: []string{"steps:\n  - addr: c\n", "steps: []\nlabels: {}\nmatrix: {}\n"},
			want: func(c *mergeConfig) {
				c.Steps = []mergeHost{}
				c.Labels = map[string]string{}
				c.Matrix = map[string]map[string]int{}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var providers []Provider
			for i, body := range tt.layers {
				providers = append(providers, File(writeFile(t, "layer"+string(rune('0'+i))+".yaml", body)))
			}
			res, err := Load(mergeDefaults(), providers...)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			want := mergeDefaults()
			tt.want(&want)
			if got := res.Value(); !reflect.DeepEqual(got, want) {
				t.Errorf("Value() =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

func TestLoadListMergedByKey(t *testing.T) {
	user := writeFile(t, "user.yaml", "profiles:\n  - name: ci\n    tags: [c]\n  - name: dev\n    meta.owner: ann\n")
	project := writeFile(t, "project.yaml", "profiles:\n  - name: qa\n  - name: prod\n    tags: [p]\n")
	res, err := Load(mergeDefaults(), File(user), File(project))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []mergeProfile{
		{Name: "dev", Meta: mergeMeta{Owner: "ann"}},
		{Name: "prod", Tags: []string{"p"}},
		{Name: "ci", Tags: []string{"c"}},
		{Name: "qa"},
	}
	if got := res.Value().Profiles; !reflect.DeepEqual(got, want) {
		t.Errorf("Profiles =\n%+v\nwant\n%+v", got, want)
	}
	origins := map[string]string{}
	for _, s := range res.Sources() {
		origins[s.Key] = s.Origin
	}
	wantOrigins := map[string]string{
		"profiles[0].name":       "user.yaml:4",
		"profiles[0].meta.owner": "user.yaml:5",
		"profiles[0].env":        "user.yaml:4",
		"profiles[1].tags":       "project.yaml:4",
		"profiles[1].hosts":      "project.yaml:3",
		"profiles[2].tags":       "user.yaml:3",
		"profiles[3].name":       "project.yaml:2",
	}
	for key, want := range wantOrigins {
		if got := origins[key]; got != want {
			t.Errorf("origin of %s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadMapMergedByKeys(t *testing.T) {
	user := writeFile(t, "user.yaml", "services:\n  web:\n    addr: w2\n  cache:\n    addr: c\n    port: 6379\n")
	project := writeFile(t, "project.yaml", "services: {}\n")
	res, err := Load(mergeDefaults(), File(user), File(project))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]mergeHost{
		"web":   {Addr: "w2"},
		"db":    {Addr: "d", Port: 5432},
		"cache": {Addr: "c", Port: 6379},
	}
	if got := res.Value().Services; !reflect.DeepEqual(got, want) {
		t.Errorf("Services = %+v, want %+v", got, want)
	}
	wantSources := []Source{
		{Key: `services["cache"].addr`, Value: "c", Origin: "user.yaml:5"},
		{Key: `services["cache"].port`, Value: "6379", Origin: "user.yaml:6"},
		{Key: `services["db"].addr`, Value: "d", Origin: "default"},
		{Key: `services["db"].port`, Value: "5432", Origin: "default"},
		{Key: `services["web"].addr`, Value: "w2", Origin: "user.yaml:3"},
		{Key: `services["web"].port`, Value: "0", Origin: "user.yaml:3"},
	}
	var got []Source
	for _, s := range res.Sources() {
		if strings.HasPrefix(s.Key, "services") {
			got = append(got, s)
		}
	}
	if !reflect.DeepEqual(got, wantSources) {
		t.Errorf("services Sources =\n%+v\nwant\n%+v", got, wantSources)
	}
}

func TestLoadCollectionsShareNothingWithDefaults(t *testing.T) {
	file := writeFile(t, "config.yaml", "services:\n  cache:\n    addr: c\nprofiles:\n  - name: qa\n")
	defaults := mergeDefaults()
	res, err := Load(defaults, File(file))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(defaults, mergeDefaults()) {
		t.Fatalf("Load changed defaults: %+v", defaults)
	}

	value := res.Value()
	value.Profiles[0].Env["FOO"] = "changed"
	value.Profiles[0].Tags[0] = "changed"
	value.Profiles[1].Hosts[0].Addr = "changed"
	value.Labels["x"] = "changed"
	value.Services["web"] = mergeHost{Addr: "changed"}
	value.Steps[0].Addr = "changed"
	value.Matrix["m"]["n"] = 9
	if !reflect.DeepEqual(defaults, mergeDefaults()) {
		t.Errorf("changing the loaded value changed defaults: %+v", defaults)
	}

	again, err := Load(defaults, File(file))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(defaults, mergeDefaults()) {
		t.Errorf("a second Load changed defaults: %+v", defaults)
	}
	if _, ok := again.Value().Services["cache"]; !ok {
		t.Errorf("Services = %+v, want the cache entry", again.Value().Services)
	}
	defaults.Profiles[0].Env["FOO"] = "later"
	if got := again.Value().Profiles[0].Env["FOO"]; got != "1" {
		t.Errorf("changing defaults changed the loaded value: FOO = %q", got)
	}
}

func TestLoadDefaultsMergeKeys(t *testing.T) {
	defaults := mergeConfig{Profiles: []mergeProfile{{Name: "dev"}, {}, {Name: "dev"}}}
	res, err := Load(defaults)
	if err == nil {
		t.Fatal("Load: no error")
	}
	want := strings.Join([]string{
		`profiles[1]: merge key "name" is empty in defaults; every element of profiles needs one`,
		`profiles[2]: merge key "name" is "dev" in defaults, as for profiles[0]`,
	}, "\n")
	if got := err.Error(); got != want {
		t.Errorf("error =\n%s\nwant\n%s", got, want)
	}
	if !reflect.DeepEqual(res, Result[mergeConfig]{}) {
		t.Errorf("Result = %+v, want the zero Result", res)
	}
}
