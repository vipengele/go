package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fileServer struct {
	Host       string
	Port       int
	MaxRetries int
}

type fileConfig struct {
	Name   string
	Server fileServer
	Tags   []string
	Ports  []int
}

func fileSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(&fileConfig{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	return s
}

// loadTestdata copies testdata/name to a file called config.yaml in a fresh directory, so that
// origins and messages name config.yaml, and loads it.
func loadTestdata(t *testing.T, name string) ([]Entry, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return File(path).Load(fileSchema(t))
}

// fileProblems splits an error from Load into the problems errors.Join combined.
func fileProblems(err error) []string {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []string
		for _, e := range joined.Unwrap() {
			out = append(out, e.Error())
		}
		return out
	}
	return []string{err.Error()}
}

func TestFileEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want []Entry
	}{
		{"nested.yaml", []Entry{
			{Key: "name", Value: "app", Origin: "config.yaml:1"},
			{Key: "server.host", Value: "example.org", Origin: "config.yaml:3"},
			{Key: "server.port", Value: "8080", Origin: "config.yaml:4"},
			{Key: "server.max_retries", Value: "3", Origin: "config.yaml:5"},
			{Key: "tags", Items: []string{"a", "b"}, List: true, Origin: "config.yaml:6"},
		}},
		{"dotted.yaml", []Entry{
			{Key: "name", Value: "app", Origin: "config.yaml:1"},
			{Key: "server.host", Value: "example.org", Origin: "config.yaml:2"},
			{Key: "server.port", Value: "8080", Origin: "config.yaml:3"},
			{Key: "server.max_retries", Value: "3", Origin: "config.yaml:4"},
			{Key: "tags", Items: []string{"a", "b"}, List: true, Origin: "config.yaml:6"},
		}},
		{"mixed.yaml", []Entry{
			{Key: "name", Value: "app", Origin: "config.yaml:1"},
			{Key: "server.host", Value: "example.org", Origin: "config.yaml:3"},
			{Key: "server.port", Value: "8080", Origin: "config.yaml:4"},
			{Key: "server.max_retries", Value: "3", Origin: "config.yaml:6"},
			{Key: "tags", Items: []string{"a", "b"}, List: true, Origin: "config.yaml:7"},
		}},
		{"lists.yaml", []Entry{
			{Key: "tags", Items: []string{"a", "b,c", ""}, List: true, Origin: "config.yaml:1"},
			{Key: "ports", Items: []string{"80", "0x1bb"}, List: true, Origin: "config.yaml:3"},
		}},
		{"empty_list.yaml", []Entry{
			{Key: "tags", Items: []string{}, List: true, Origin: "config.yaml:1"},
		}},
		{"raw.yaml", []Entry{
			{Key: "name", Value: "  quoted  ", Origin: "config.yaml:1"},
			{Key: "server.port", Value: "0080", Origin: "config.yaml:2"},
			{Key: "server.host", Value: "yes", Origin: "config.yaml:3"},
		}},
		// An alias yields the anchored node, so its origin is the line of the anchored value.
		{"alias.yaml", []Entry{
			{Key: "name", Value: "demo", Origin: "config.yaml:1"},
			{Key: "server.host", Value: "demo", Origin: "config.yaml:1"},
		}},
		{"empty.yaml", nil},
		{"comments.yaml", nil},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			got, err := loadTestdata(t, tt.file)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Load entries:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestFileNestedAndDottedKeysAreOneKeySpace(t *testing.T) {
	t.Parallel()
	values := func(entries []Entry) map[string]Entry {
		out := make(map[string]Entry, len(entries))
		for _, e := range entries {
			e.Origin = ""
			out[e.Key] = e
		}
		return out
	}
	nested, err := loadTestdata(t, "nested.yaml")
	if err != nil {
		t.Fatalf("Load nested.yaml: %v", err)
	}
	for _, file := range []string{"dotted.yaml", "mixed.yaml"} {
		got, err := loadTestdata(t, file)
		if err != nil {
			t.Fatalf("Load %s: %v", file, err)
		}
		if !reflect.DeepEqual(values(got), values(nested)) {
			t.Errorf("%s sets %+v, nested.yaml sets %+v", file, values(got), values(nested))
		}
	}
}

func TestFileProblems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want []string // one substring per problem, in order
	}{
		{"duplicate.yaml", []string{
			`config.yaml:3: key "name" sets the same key as "name" at line 1`,
		}},
		{"duplicate_relaxed.yaml", []string{
			`config.yaml:3: key "server.max-retries" sets the same key as "server.max_retries" at line 2`,
		}},
		{"duplicate_nested.yaml", []string{
			`config.yaml:4: key "server.port" sets the same key as "server.port" at line 1`,
		}},
		{"unknown.yaml", []string{
			`config.yaml:4: unknown key "server.prot"`,
		}},
		{"multiple.yaml", []string{
			`config.yaml:2: unknown key "colour"`,
			`config.yaml:5: key "server.port" sets the same key as "server.port" at line 4`,
			`config.yaml:6: unknown key "server.timeout"`,
		}},
		{"shape.yaml", []string{
			`config.yaml:1: key "server" names a group of keys, not a value; it takes a mapping, not the value "80"`,
			`config.yaml:3: key "name" is a mapping, but it holds a single string`,
			`config.yaml:4: key "server.port" is a list, but it holds a single int`,
			`config.yaml:7: an element of key "tags" is a mapping, not a single value`,
			`config.yaml:9: an element of key "ports" is a list, not a single value`,
		}},
		{"null.yaml", []string{
			`config.yaml:1: key "name" is null`,
			`config.yaml:2: key "server.host" is null`,
			`config.yaml:3: an element of key "tags" is null`,
		}},
		// A merge key is an ordinary key named "<<", which no Schema has.
		{"merge_key.yaml", []string{
			`config.yaml:3: unknown key "<<"`,
		}},
		{"syntax.yaml", []string{
			`config.yaml: yaml:`,
		}},
		{"top_list.yaml", []string{
			`config.yaml: line 1: the top level is a list, not a mapping of keys`,
		}},
		{"two_documents.yaml", []string{
			`config.yaml: line 2: a second YAML document`,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			entries, err := loadTestdata(t, tt.file)
			if err == nil {
				t.Fatalf("Load = %+v, want an error", entries)
			}
			if entries != nil {
				t.Errorf("Load entries = %+v alongside an error, want none", entries)
			}
			got := fileProblems(err)
			if len(got) != len(tt.want) {
				t.Fatalf("Load reported %d problems, want %d:\n%s", len(got), len(tt.want), strings.Join(got, "\n"))
			}
			for i, want := range tt.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], want)
				}
			}
		})
	}
}

func TestFileReadErrorsNameThePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := File(dir).Load(fileSchema(t))
	if err == nil {
		t.Fatal("Load of a directory succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "config file "+dir) {
		t.Errorf("Load error = %q, want it to name %s", err, dir)
	}
}

func TestFileMissingSetsNothing(t *testing.T) {
	t.Parallel()
	entries, err := File(filepath.Join(t.TempDir(), "absent.yaml")).Load(fileSchema(t))
	if err != nil || entries != nil {
		t.Errorf("Load of a missing file = %+v, %v; want no entries and no error", entries, err)
	}
}

func TestFileOriginsNameTheBaseName(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "etc", "app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "settings.yml")
	if err := os.WriteFile(path, []byte("# settings\n\nname: app\nnope: 1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := File(path).Load(fileSchema(t))
	if want := `settings.yml:4: unknown key "nope"`; err == nil || err.Error() != want {
		t.Errorf("Load error = %v, want %q", err, want)
	}

	if err := os.WriteFile(path, []byte("# settings\n\nname: app\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := File(path).Load(fileSchema(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 1 || entries[0].Origin != "settings.yml:3" {
		t.Errorf("Load entries = %+v, want one with origin settings.yml:3", entries)
	}
}

func TestFileIsStrict(t *testing.T) {
	t.Parallel()
	if !File("config.yaml").Strict() {
		t.Error("File(...).Strict() = false, want true")
	}
}
