package config

import (
	"reflect"
	"strings"
	"testing"
)

type flagServer struct {
	Host       string
	MaxRetries int
}

type flagConfig struct {
	Name   string
	Server flagServer
	Tags   []string
}

func (flagConfig) RemovedKeys() map[string]string {
	return map[string]string{
		"legacy":           "drop it",
		"server.max_conns": "use server.max-retries",
	}
}

func flagSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(&flagConfig{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	return s
}

func TestFlagsLoad(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   []Entry
	}{
		{name: "nil map"},
		{name: "empty map", values: map[string]string{}},
		{
			name:   "canonical key",
			values: map[string]string{"name": "x"},
			want:   []Entry{{Key: "name", Value: "x", Origin: "flag --name"}},
		},
		{
			name:   "empty value is a value",
			values: map[string]string{"name": ""},
			want:   []Entry{{Key: "name", Value: "", Origin: "flag --name"}},
		},
		{
			name:   "dashes find underscores",
			values: map[string]string{"server.max-retries": "3"},
			want:   []Entry{{Key: "server.max_retries", Value: "3", Origin: "flag --server.max-retries"}},
		},
		{
			name:   "case and separators are ignored in every segment",
			values: map[string]string{"Server.Max-Retries": "3", "NAME": "n"},
			want: []Entry{
				{Key: "name", Value: "n", Origin: "flag --name"},
				{Key: "server.max_retries", Value: "3", Origin: "flag --server.max-retries"},
			},
		},
		{
			name:   "camel case spelling",
			values: map[string]string{"server.maxRetries": "3"},
			want:   []Entry{{Key: "server.max_retries", Value: "3", Origin: "flag --server.max-retries"}},
		},
		{
			name:   "slice stays one comma-separated value",
			values: map[string]string{"tags": "a,b"},
			want:   []Entry{{Key: "tags", Value: "a,b", Origin: "flag --tags"}},
		},
		{
			name:   "entries follow schema order",
			values: map[string]string{"tags": "t", "server.max-retries": "3", "server.host": "h", "name": "n"},
			want: []Entry{
				{Key: "name", Value: "n", Origin: "flag --name"},
				{Key: "server.host", Value: "h", Origin: "flag --server.host"},
				{Key: "server.max_retries", Value: "3", Origin: "flag --server.max-retries"},
				{Key: "tags", Value: "t", Origin: "flag --tags"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Map iteration order is random; repeating exposes any dependence on it.
			for range 20 {
				got, err := Flags(tt.values).Load(flagSchema(t))
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("entries = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestFlagsIsStrict(t *testing.T) {
	if !Flags(nil).Strict() {
		t.Error("Strict() = false, want true")
	}
}

func TestFlagsErrors(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   []string
	}{
		{
			name:   "unknown key",
			values: map[string]string{"nope": "1"},
			want:   []string{"flag --nope: unknown key"},
		},
		{
			name:   "a group is not a key",
			values: map[string]string{"server": "1"},
			want:   []string{"flag --server: unknown key"},
		},
		{
			name:   "every unknown key, sorted",
			values: map[string]string{"zeta": "1", "alpha": "2", "name": "ok", "mid": "3"},
			want: []string{
				"flag --alpha: unknown key",
				"flag --mid: unknown key",
				"flag --zeta: unknown key",
			},
		},
		{
			name:   "removed key carries its hint",
			values: map[string]string{"legacy": "1"},
			want:   []string{"flag --legacy: key was removed: drop it"},
		},
		{
			name:   "removed key in another spelling is named as written",
			values: map[string]string{"server.max-conns": "1"},
			want:   []string{"flag --server.max-conns: key was removed: use server.max-retries"},
		},
		{
			name:   "removed and unknown keys together, sorted",
			values: map[string]string{"zeta": "1", "legacy": "2", "server.max-conns": "3", "alpha": "4", "name": "ok"},
			want: []string{
				"flag --alpha: unknown key",
				"flag --legacy: key was removed: drop it",
				"flag --server.max-conns: key was removed: use server.max-retries",
				"flag --zeta: unknown key",
			},
		},
		{
			name:   "a key below a removed key is unknown",
			values: map[string]string{"legacy.x": "1"},
			want:   []string{"flag --legacy.x: unknown key"},
		},
		{
			name:   "two spellings of one field",
			values: map[string]string{"server.max-retries": "1", "server.max_retries": "2"},
			want:   []string{"flag --server.max_retries sets the same key as --server.max-retries"},
		},
		{
			name:   "duplicates and unknowns together",
			values: map[string]string{"name": "1", "NAME": "2", "bad": "3"},
			want: []string{
				"flag --bad: unknown key",
				"flag --name sets the same key as --NAME",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 20 {
				got, err := Flags(tt.values).Load(flagSchema(t))
				if err == nil {
					t.Fatalf("Load = %#v, want an error", got)
				}
				if problems := fileProblems(err); !reflect.DeepEqual(problems, tt.want) {
					t.Fatalf("problems = %q, want %q", problems, tt.want)
				}
			}
		})
	}
}

func TestFlagsValidKeyLoadsBesideSchemaRemovals(t *testing.T) {
	got, err := Flags(map[string]string{"server.host": "h"}).Load(flagSchema(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Entry{{Key: "server.host", Value: "h", Origin: "flag --server.host"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestFlagsUnknownKeyIsNamed(t *testing.T) {
	_, err := Flags(map[string]string{"Bogus.Key": "1"}).Load(flagSchema(t))
	if err == nil || !strings.Contains(err.Error(), "Bogus.Key") {
		t.Errorf("error = %v, want it to name Bogus.Key", err)
	}
}
