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
