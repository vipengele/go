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
