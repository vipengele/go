package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func (fileConfig) RemovedKeys() map[string]string {
	return map[string]string{
		"legacy":           "drop it",
		"server.max_conns": "use server.max_retries",
		"old_settings":     "settings moved to server",
	}
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
// origins and messages name config.yaml, and loads it against the Schema of fileConfig.
func loadTestdata(t *testing.T, name string) ([]Entry, error) {
	t.Helper()
	return loadTestdataWith(t, name, fileSchema(t))
}

// loadTestdataWith loads testdata/name as loadTestdata does, against schema.
func loadTestdataWith(t *testing.T, name string, schema *Schema) ([]Entry, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return File(path).Load(schema)
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
		{"removed.yaml", []string{
			`config.yaml:2: key "legacy" was removed: drop it`,
			`config.yaml:4: key "server.max_conns" was removed: use server.max_retries`,
			`config.yaml:6: key "Old-Settings" was removed: settings moved to server`,
			`config.yaml:9: unknown key "colour"`,
		}},
		{"syntax.yaml", []string{
			`config.yaml: yaml:`,
		}},
		{"complex_key.yaml", []string{
			`config.yaml:2: a key below "server" is a list, not a name`,
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

func TestFileRemovedKeyDottedAndMapped(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "config.yaml", "server.max-conns: 4\nlegacy:\n  deep:\n    - 1\n")
	_, err := File(path).Load(fileSchema(t))
	want := []string{
		`config.yaml:1: key "server.max-conns" was removed: use server.max_retries`,
		`config.yaml:2: key "legacy" was removed: drop it`,
	}
	if got := fileProblems(err); !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}
}

func TestEnvIgnoresRemovedKey(t *testing.T) {
	t.Parallel()
	env := mapLookup(map[string]string{"APP_LEGACY": "1", "APP_SERVER_MAX_CONNS": "4", "APP_NAME": "n"})
	got, err := Env("APP", env).Load(fileSchema(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Entry{{Key: "name", Value: "n", Origin: "env APP_NAME"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %#v, want %#v", got, want)
	}
}

type fileMeta struct {
	Owner string
}

type fileHost struct {
	Addr string
}

type fileProfile struct {
	Name  string
	Meta  fileMeta
	Env   map[string]string
	Tags  []string
	Hosts []fileHost
}

type fileCollections struct {
	Name     string
	Profiles []fileProfile     `merge:"name"`
	ByOwner  []fileProfile     `merge:"meta.owner"`
	Labels   map[string]string `merge:"keys"`
	Matrix   map[string]map[string]int
	Services map[string]fileHost
	Steps    []fileHost
}

func collectionsSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(&fileCollections{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	return s
}

// at is the origin of line in config.yaml.
func at(line int) string { return "config.yaml:" + strconv.Itoa(line) }

func wantScalar(value string, line int) *Node {
	return &Node{Kind: NodeScalar, Value: value, Origin: at(line)}
}

func wantList(line int, items ...*Node) *Node {
	return &Node{Kind: NodeList, Items: append([]*Node{}, items...), Origin: at(line)}
}

func wantMap(line int, entries ...NodeEntry) *Node {
	return &Node{Kind: NodeMap, Entries: append([]NodeEntry{}, entries...), Origin: at(line)}
}

func TestFileCollectionNodes(t *testing.T) {
	t.Parallel()
	got, err := loadTestdataWith(t, "collections.yaml", collectionsSchema(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	profiles := wantList(3,
		wantMap(3,
			NodeEntry{"name", wantScalar("dev", 3)},
			NodeEntry{"meta.owner", wantScalar("ann", 5)},
			// A map key is verbatim: "a.b" is one key, never split into a level.
			NodeEntry{"env", wantMap(7,
				NodeEntry{"FOO", wantScalar("1", 7)},
				NodeEntry{"a.b", wantScalar("two", 8)},
			)},
			NodeEntry{"tags", wantList(9, wantScalar("x", 9), wantScalar("y", 9))},
		),
		wantMap(10,
			NodeEntry{"name", wantScalar("prod", 10)},
			NodeEntry{"meta.owner", wantScalar("bob", 10)},
			NodeEntry{"env", wantMap(10)},
		),
		wantMap(11,
			NodeEntry{"name", wantScalar("ci", 11)},
			NodeEntry{"hosts", wantList(13,
				wantMap(13, NodeEntry{"addr", wantScalar("h1", 13)}),
				wantMap(14),
			)},
		),
	)
	matrix := wantMap(17,
		NodeEntry{"a", wantMap(18, NodeEntry{"b", wantScalar("1", 18)})},
		NodeEntry{"c", wantMap(19)},
	)
	services := wantMap(21,
		NodeEntry{"web.app", wantMap(22, NodeEntry{"addr", wantScalar("w", 22)})},
		NodeEntry{"empty", wantMap(23)},
	)
	want := []Entry{
		{Key: "name", Value: "app", Origin: at(1)},
		{Key: "profiles", Node: profiles, Origin: at(3)},
		{Key: "labels", Node: wantMap(15), Origin: at(15)},
		{Key: "matrix", Node: matrix, Origin: at(17)},
		{Key: "services", Node: services, Origin: at(21)},
		{Key: "steps", Node: wantList(24), Origin: at(24)},
	}
	if len(got) != len(want) {
		t.Fatalf("Load returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("entry %d:\n got %s\nwant %s", i, dumpEntry(got[i]), dumpEntry(want[i]))
		}
	}
}

// dumpEntry renders e with its Node tree, for a failure message.
func dumpEntry(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%+v", Entry{Key: e.Key, Value: e.Value, Items: e.Items, List: e.List, Origin: e.Origin})
	if e.Node != nil {
		dumpNode(&b, e.Node, "  ")
	}
	return b.String()
}

func dumpNode(b *strings.Builder, n *Node, indent string) {
	fmt.Fprintf(b, "\n%s%s %q at %s (items %v, entries %v)", indent, n.Kind, n.Value, n.Origin, n.Items != nil, n.Entries != nil)
	for _, item := range n.Items {
		dumpNode(b, item, indent+"  ")
	}
	for _, e := range n.Entries {
		fmt.Fprintf(b, "\n%s  [%q]", indent, e.Key)
		dumpNode(b, e.Node, indent+"    ")
	}
}

func TestFileCollectionProblems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want []string // one problem each, in order
	}{
		// Keys within an element are strict, nested and dotted alike; keys beneath a map are not
		// matched, but the keys of the struct a map holds are.
		{"collection_unknown.yaml", []string{
			`config.yaml:3: profiles[0]: unknown key "nmae"`,
			`config.yaml:5: profiles[0]: unknown key "meta.ownr"`,
			`config.yaml:7: profiles[0]: key "Meta.Owner" sets the same key as "meta.owner" at line 6`,
			`config.yaml:10: services["web"]: unknown key "port"`,
		}},
		{"collection_merge_key.yaml", []string{
			`config.yaml:4: profiles[2]: merge key "name" is "dev", as for profiles[0] at line 2`,
			`config.yaml:5: profiles[3]: merge key "name" is empty; every element of profiles needs one`,
			`config.yaml:6: profiles[4]: merge key "name" is empty; every element of profiles needs one`,
			`config.yaml:9: by_owner[1]: merge key "meta.owner" is "ann", as for by_owner[0] at line 8`,
		}},
		{"collection_shape.yaml", []string{
			`config.yaml:1: profiles is the value "x", but it holds a list of config.fileProfile`,
			`config.yaml:2: labels is a list, but it holds a mapping of string`,
			`config.yaml:4: steps[0].addr is a list, but it holds a single string`,
			`config.yaml:5: steps[1] is the value "x", but it holds the keys of config.fileHost`,
			`config.yaml:6: steps[2] is null; give it a value or remove it`,
			`config.yaml:8: matrix["a"] is the value "1", but it holds a mapping of int`,
			`config.yaml:9: matrix["b"]: key "c" is set again; it is first set at line 9`,
			`config.yaml:10: key "services" is null; give it a value or remove it`,
			`config.yaml:13: by_owner[0].tags[0] is a list, not a single value`,
			`config.yaml:13: by_owner[0].tags[1] is null; give it a value or remove it`,
		}},
		// A key beneath a map is taken verbatim, but only a scalar is a key: a list or a mapping is not.
		{"collection_complex_key.yaml", []string{
			`config.yaml:2: labels: a key is a list, not a name`,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			entries, err := loadTestdataWith(t, tt.file, collectionsSchema(t))
			if entries != nil {
				t.Errorf("Load entries = %+v alongside an error, want none", entries)
			}
			if got := fileProblems(err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("problems:\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(tt.want, "\n     "))
			}
		})
	}
}

func TestFileCollectionDistinctMergeKeysLoad(t *testing.T) {
	t.Parallel()
	schema := collectionsSchema(t)
	for _, body := range []string{"profiles:\n  - name: dev\n", "profiles:\n  - name: dev\n  - name: prod\n"} {
		entries, err := File(writeFile(t, "config.yaml", body)).Load(schema)
		if err != nil || len(entries) != 1 || entries[0].Node == nil {
			t.Errorf("Load(%q) = %+v, %v; want one collection entry", body, entries, err)
		}
	}
}

func TestNodeKindString(t *testing.T) {
	t.Parallel()
	for kind, want := range map[NodeKind]string{NodeScalar: "scalar", NodeList: "list", NodeMap: "map"} {
		if got := kind.String(); got != want {
			t.Errorf("NodeKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}
