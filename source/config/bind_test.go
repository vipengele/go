package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type bindLevel int

type bindConfig struct {
	B     bool
	I     int
	I8    int8
	U16   uint16
	F32   float32
	F64   float64
	S     string
	D     time.Duration
	Level bindLevel
	Ints  []int
	Strs  []string
	Durs  []time.Duration
}

func TestBindConversions(t *testing.T) {
	tests := []struct {
		name    string
		entries []Entry
		want    bindConfig
	}{
		{
			name: "single values",
			entries: []Entry{
				{Key: "b", Value: "true"},
				{Key: "i", Value: "-42"},
				{Key: "i8", Value: "-128"},
				{Key: "u16", Value: "65535"},
				{Key: "f32", Value: "1.5"},
				{Key: "f64", Value: "2.25"},
				{Key: "s", Value: " spaced "},
				{Key: "d", Value: "1m30s"},
				{Key: "level", Value: "3"},
			},
			want: bindConfig{B: true, I: -42, I8: -128, U16: 65535, F32: 1.5, F64: 2.25, S: " spaced ", D: 90 * time.Second, Level: 3},
		},
		{
			name:    "list items are taken as written",
			entries: []Entry{{Key: "strs", Items: []string{" a", "b,c"}, List: true}, {Key: "ints", Items: []string{"1", "2"}, List: true}},
			want:    bindConfig{Strs: []string{" a", "b,c"}, Ints: []int{1, 2}},
		},
		{
			name:    "comma text is split and trimmed",
			entries: []Entry{{Key: "strs", Value: " a , b,c "}, {Key: "durs", Value: "1s, 2m"}},
			want:    bindConfig{Strs: []string{"a", "b", "c"}, Durs: []time.Duration{time.Second, 2 * time.Minute}},
		},
		{
			name:    "empty text is an empty slice",
			entries: []Entry{{Key: "strs", Value: ""}, {Key: "ints", Value: ""}},
			want:    bindConfig{Strs: []string{}, Ints: []int{}},
		},
		{
			name:    "empty list is an empty slice",
			entries: []Entry{{Key: "strs", Items: []string{}, List: true}},
			want:    bindConfig{Strs: []string{}},
		},
		{
			name:    "a later entry replaces the slice",
			entries: []Entry{{Key: "ints", Items: []string{"1", "2", "3"}, List: true}, {Key: "ints", Value: "9"}},
			want:    bindConfig{Ints: []int{9}},
		},
		{
			name:    "key matches ignoring case and separators",
			entries: []Entry{{Key: "LEVEL", Value: "2"}},
			want:    bindConfig{Level: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Load(bindConfig{}, &stubProvider{entries: tt.entries})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := res.Value(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Value() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBindSliceReplacesAcrossLayers(t *testing.T) {
	path := writeFile(t, "config.yaml", "tags:\n  - x\n  - y\n")
	res, err := Load(loadDefaults(), File(path), Env("APP", mapLookup(map[string]string{"APP_TAGS": "z"})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := res.Value().Tags, []string{"z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tags = %q, want %q", got, want)
	}
}

func TestBindConversionErrors(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  []string
	}{
		{name: "bool", entry: Entry{Key: "b", Value: "yes", Origin: "o"}, want: []string{`b: invalid bool "yes" from o`}},
		{name: "int", entry: Entry{Key: "i", Value: "1.5", Origin: "o"}, want: []string{`i: invalid int "1.5" from o`}},
		{name: "int8 out of range", entry: Entry{Key: "i8", Value: "128", Origin: "o"}, want: []string{`i8: invalid int8 "128" from o: out of range`}},
		{name: "uint16 out of range", entry: Entry{Key: "u16", Value: "65536", Origin: "o"}, want: []string{`u16: invalid uint16 "65536" from o: out of range`}},
		{name: "negative uint", entry: Entry{Key: "u16", Value: "-1", Origin: "o"}, want: []string{`u16: invalid uint16 "-1" from o`}},
		{name: "float32 out of range", entry: Entry{Key: "f32", Value: "1e39", Origin: "o"}, want: []string{`f32: invalid float32 "1e39" from o: out of range`}},
		{name: "duration", entry: Entry{Key: "d", Value: "30", Origin: "o"}, want: []string{`d: invalid duration "30" from o`}},
		{name: "named type by kind", entry: Entry{Key: "level", Value: "high", Origin: "o"}, want: []string{`level: invalid int "high" from o`}},
		{
			name:  "every bad element",
			entry: Entry{Key: "ints", Items: []string{"1", "x", "3", "y"}, List: true, Origin: "o"},
			want:  []string{`ints: invalid int "x" (element 2) from o`, `ints: invalid int "y" (element 4) from o`},
		},
		{
			name:  "bad element of comma text",
			entry: Entry{Key: "durs", Value: "1s,,2s", Origin: "o"},
			want:  []string{`durs: invalid duration "" (element 2) from o`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(bindConfig{}, &stubProvider{entries: []Entry{tt.entry}})
			if err == nil {
				t.Fatal("Load: no error")
			}
			if got, want := err.Error(), strings.Join(tt.want, "\n"); got != want {
				t.Errorf("error =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

type bindRemovedConfig struct {
	Name string
}

func (bindRemovedConfig) RemovedKeys() map[string]string {
	return map[string]string{"legacy": "drop it"}
}

func TestBindRemovedKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "removed key", key: "legacy", want: "legacy: key was removed: drop it from custom"},
		{name: "removed key, other spelling", key: "LEGACY", want: "LEGACY: key was removed: drop it from custom"},
		{name: "unknown key", key: "nope", want: "nope: unknown key from custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &stubProvider{entries: []Entry{{Key: tt.key, Value: "1", Origin: "custom"}}}
			_, err := Load(bindRemovedConfig{}, p)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func stubScalar(value, origin string) *Node {
	return &Node{Kind: NodeScalar, Value: value, Origin: origin}
}

func stubMap(origin string, entries ...NodeEntry) *Node {
	return &Node{Kind: NodeMap, Entries: append([]NodeEntry{}, entries...), Origin: origin}
}

func stubList(origin string, items ...*Node) *Node {
	return &Node{Kind: NodeList, Items: append([]*Node{}, items...), Origin: origin}
}

func TestBindCollectionFromNodes(t *testing.T) {
	entries := []Entry{
		{Key: "profiles", Origin: "s:1", Node: stubList("s:1",
			stubMap("s:2",
				NodeEntry{Key: "name", Node: stubScalar("ci", "s:2")},
				NodeEntry{Key: "hosts", Node: stubList("s:3", stubMap("s:3", NodeEntry{Key: "port", Node: stubScalar("8", "s:3")}))},
				NodeEntry{Key: "tags", Node: stubScalar("a, b", "s:4")},
			),
		)},
		{Key: "matrix", Origin: "s:5", Node: stubMap("s:5", NodeEntry{Key: "a.b", Node: stubMap("s:6", NodeEntry{Key: "C", Node: stubScalar("3", "s:6")})})},
	}
	res, err := Load(mergeConfig{}, &stubProvider{entries: entries})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := res.Value()
	wantProfiles := []mergeProfile{{Name: "ci", Hosts: []mergeHost{{Port: 8}}, Tags: []string{"a", "b"}}}
	if !reflect.DeepEqual(got.Profiles, wantProfiles) {
		t.Errorf("Profiles = %+v, want %+v", got.Profiles, wantProfiles)
	}
	if want := map[string]map[string]int{"a.b": {"C": 3}}; !reflect.DeepEqual(got.Matrix, want) {
		t.Errorf("Matrix = %+v, want %+v", got.Matrix, want)
	}
}

func TestBindCollectionErrors(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  []string
	}{
		{
			name:  "no node",
			entry: Entry{Key: "steps", Value: "x", Origin: "o"},
			want:  []string{"steps: no node from o, but it holds a list of config.mergeHost"},
		},
		{
			name:  "node for a single value",
			entry: Entry{Key: "name", Node: stubScalar("x", "o"), Origin: "o"},
			want:  []string{"name: a scalar node from o, but it holds a single string"},
		},
		{
			name:  "shape of the collection",
			entry: Entry{Key: "labels", Node: stubList("o:1"), Origin: "o:1"},
			want:  []string{"labels: a list node from o:1, but it holds a mapping of string"},
		},
		{
			name: "every value that does not convert, by path within the layer",
			entry: Entry{Key: "profiles", Origin: "o:1", Node: stubList("o:1",
				stubMap("o:2", NodeEntry{Key: "name", Node: stubScalar("a", "o:2")}),
				stubMap("o:3",
					NodeEntry{Key: "name", Node: stubScalar("b", "o:3")},
					NodeEntry{Key: "hosts", Node: stubList("o:4", stubMap("o:4", NodeEntry{Key: "port", Node: stubScalar("x", "o:4")}))},
					NodeEntry{Key: "tags", Node: stubList("o:5", stubMap("o:5"))},
					NodeEntry{Key: "nope", Node: stubScalar("1", "o:6")},
					NodeEntry{Key: "meta.owner", Node: nil},
				),
			)},
			want: []string{
				`profiles[1].hosts[0].port: invalid int "x" from o:4`,
				"profiles[1].tags: a list node from o:5, but it holds a list of string",
				"profiles[1].nope: unknown key from o:3",
				"profiles[1].meta.owner: no node from o:3",
			},
		},
		{
			name:  "bad element of comma text within an element",
			entry: Entry{Key: "steps", Origin: "o:1", Node: stubList("o:1", stubMap("o:1", NodeEntry{Key: "port", Node: stubList("o:2", stubScalar("1", "o:2"))}))},
			want:  []string{"steps[0].port: a list node from o:2, but it holds a single int"},
		},
		{
			name: "map entry and list item with no node",
			entry: Entry{Key: "services", Origin: "o:1", Node: stubMap("o:1",
				NodeEntry{Key: "web", Node: nil},
				NodeEntry{Key: "db", Node: stubMap("o:2", NodeEntry{Key: "port", Node: stubScalar("99999999999999999999", "o:2")})},
			)},
			want: []string{
				`services["web"]: no node from o:1`,
				`services["db"].port: invalid int "99999999999999999999" from o:2: out of range`,
			},
		},
		{
			name:  "map node for a list of elements",
			entry: Entry{Key: "steps", Node: stubMap("o:1"), Origin: "o:1"},
			want:  []string{"steps: a map node from o:1, but it holds a list of config.mergeHost"},
		},
		{
			name:  "scalar node for an element",
			entry: Entry{Key: "steps", Origin: "o:1", Node: stubList("o:1", stubScalar("x", "o:2"))},
			want:  []string{"steps[0]: a scalar node from o:2, but it holds the keys of config.mergeHost"},
		},
		{
			name: "map node for a list of single values within an element",
			entry: Entry{Key: "profiles", Origin: "o:1", Node: stubList("o:1",
				stubMap("o:2",
					NodeEntry{Key: "name", Node: stubScalar("a", "o:2")},
					NodeEntry{Key: "tags", Node: stubMap("o:3")},
				),
			)},
			want: []string{"profiles[0].tags: a map node from o:3, but it holds a list of string"},
		},
		{
			name:  "list item with no node",
			entry: Entry{Key: "steps", Origin: "o:1", Node: &Node{Kind: NodeList, Items: []*Node{nil}, Origin: "o:1"}},
			want:  []string{"steps[0]: no node from o:1"},
		},
		{
			name: "merge keys of a layer",
			entry: Entry{Key: "profiles", Origin: "o:1", Node: stubList("o:1",
				stubMap("o:2", NodeEntry{Key: "name", Node: stubScalar("a", "o:2")}),
				stubMap("o:3"),
				stubMap("o:4", NodeEntry{Key: "name", Node: stubScalar("a", "o:4")}),
			)},
			want: []string{
				`profiles[1]: merge key "name" is empty from o:3; every element of profiles needs one`,
				`profiles[2]: merge key "name" is "a" from o:4, as for profiles[0]`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mergeConfig{}, &stubProvider{entries: []Entry{tt.entry}})
			if err == nil {
				t.Fatal("Load: no error")
			}
			if got, want := err.Error(), strings.Join(tt.want, "\n"); got != want {
				t.Errorf("error =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestBindCollectionErrorLeavesEarlierLayer(t *testing.T) {
	good := &stubProvider{entries: []Entry{{Key: "labels", Origin: "g", Node: stubMap("g", NodeEntry{Key: "k", Node: stubScalar("v", "g")})}}}
	bad := &stubProvider{entries: []Entry{{Key: "labels", Origin: "b", Node: stubScalar("x", "b")}}}
	_, err := Load(mergeConfig{}, good, bad)
	if err == nil || !strings.Contains(err.Error(), "labels: a scalar node from b") {
		t.Errorf("Load error = %v, want the shape of labels from b", err)
	}
}
