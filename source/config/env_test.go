package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

type envServer struct {
	Host       string
	MaxRetries int
}

type envConfig struct {
	Name   string
	Server envServer
	Tags   []string
}

func envSchema(t *testing.T, prefix string) *Schema {
	t.Helper()
	s, err := NewSchema(&envConfig{}, prefix)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	return s
}

func mapLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func TestEnvLoad(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		env    map[string]string
		want   []Entry
	}{
		{name: "nothing set", prefix: "APP", env: map[string]string{}},
		{
			name:   "found",
			prefix: "APP",
			env:    map[string]string{"APP_NAME": "x"},
			want:   []Entry{{Key: "name", Value: "x", Origin: "env APP_NAME"}},
		},
		{
			name:   "empty value is a value",
			prefix: "APP",
			env:    map[string]string{"APP_NAME": ""},
			want:   []Entry{{Key: "name", Value: "", Origin: "env APP_NAME"}},
		},
		{
			name:   "nested keys in schema order",
			prefix: "APP",
			env:    map[string]string{"APP_SERVER_MAX_RETRIES": "3", "APP_SERVER_HOST": "h", "APP_NAME": "n"},
			want: []Entry{
				{Key: "name", Value: "n", Origin: "env APP_NAME"},
				{Key: "server.host", Value: "h", Origin: "env APP_SERVER_HOST"},
				{Key: "server.max_retries", Value: "3", Origin: "env APP_SERVER_MAX_RETRIES"},
			},
		},
		{
			name:   "slice stays one comma-separated value",
			prefix: "APP",
			env:    map[string]string{"APP_TAGS": "a,b,c"},
			want:   []Entry{{Key: "tags", Value: "a,b,c", Origin: "env APP_TAGS"}},
		},
		{
			name:   "stray prefixed variables are ignored",
			prefix: "APP",
			env:    map[string]string{"APP_UNKNOWN": "1", "APP_SERVER": "2", "APP_SERVER_PORT": "3", "APP_NAME": "x"},
			want:   []Entry{{Key: "name", Value: "x", Origin: "env APP_NAME"}},
		},
		{
			name:   "other prefix is not read",
			prefix: "APP",
			env:    map[string]string{"OTHER_NAME": "x", "NAME": "y"},
		},
		{
			name:   "empty prefix",
			prefix: "",
			env:    map[string]string{"NAME": "x", "SERVER_HOST": "h", "APP_NAME": "z"},
			want: []Entry{
				{Key: "name", Value: "x", Origin: "env NAME"},
				{Key: "server.host", Value: "h", Origin: "env SERVER_HOST"},
			},
		},
		{
			name:   "prefix with digits and underscores",
			prefix: "MY_APP2",
			env:    map[string]string{"MY_APP2_NAME": "x"},
			want:   []Entry{{Key: "name", Value: "x", Origin: "env MY_APP2_NAME"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Env(tt.prefix, mapLookup(tt.env)).Load(envSchema(t, tt.prefix))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("entries = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEnvPrefixIsAuthoritative(t *testing.T) {
	got, err := Env("NEW", mapLookup(map[string]string{"NEW_NAME": "x", "OLD_NAME": "y"})).Load(envSchema(t, "OLD"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Entry{{Key: "name", Value: "x", Origin: "env NEW_NAME"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %#v, want %#v", got, want)
	}
}

func TestEnvIsLenient(t *testing.T) {
	if Env("APP", mapLookup(nil)).Strict() {
		t.Error("Strict() = true, want false")
	}
}

func TestEnvDoesNotReadProcessEnvironment(t *testing.T) {
	t.Setenv("APP_NAME", "decoy")
	t.Setenv("APP_SERVER_HOST", "decoy")
	got, err := Env("APP", mapLookup(map[string]string{})).Load(envSchema(t, "APP"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("entries = %#v, want none", got)
	}
	if os.Getenv("APP_NAME") != "decoy" {
		t.Fatal("decoy not set")
	}
}

func TestEnvLookupCalledOnlyForSchemaNames(t *testing.T) {
	var asked []string
	_, err := Env("APP", func(name string) (string, bool) {
		asked = append(asked, name)
		return "", false
	}).Load(envSchema(t, "APP"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"APP_NAME", "APP_SERVER_HOST", "APP_SERVER_MAX_RETRIES", "APP_TAGS"}
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("looked up %v, want %v", asked, want)
	}
}

func TestEnvErrors(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		lookup func(string) (string, bool)
		want   string
	}{
		{name: "nil lookup", prefix: "APP", lookup: nil, want: "lookup is nil"},
		{name: "lower-case prefix", prefix: "app", lookup: mapLookup(nil), want: `prefix "app"`},
		{name: "trailing underscore prefix", prefix: "APP_", lookup: mapLookup(nil), want: `prefix "APP_"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Env(tt.prefix, tt.lookup).Load(envSchema(t, "APP"))
			if err == nil {
				t.Fatalf("Load = %#v, want an error", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}
