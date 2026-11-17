package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type schemaServer struct {
	Host       string
	Port       int `config:"port"`
	MaxRetries uint8
	Timeout    time.Duration `config:"read-timeout"`
}

type schemaLimits struct {
	Burst float64
}

type schemaRate struct {
	Limits schemaLimits `config:"limits"`
}

type schemaConfig struct {
	Verbose bool
	Server  schemaServer
	Rate    schemaRate `config:"rateLimit"`
	Tags    []string
	Skipped string `config:"-"`
	hidden  string
}

func TestNewSchemaFields(t *testing.T) {
	s, err := NewSchema(&schemaConfig{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	type want struct {
		key, env, flag string
		typ            reflect.Type
		goField        string
	}
	wants := []want{
		{"verbose", "APP_VERBOSE", "verbose", reflect.TypeFor[bool](), "Verbose"},
		{"server.host", "APP_SERVER_HOST", "server.host", reflect.TypeFor[string](), "Host"},
		{"server.port", "APP_SERVER_PORT", "server.port", reflect.TypeFor[int](), "Port"},
		{"server.max_retries", "APP_SERVER_MAX_RETRIES", "server.max-retries", reflect.TypeFor[uint8](), "MaxRetries"},
		{"server.read_timeout", "APP_SERVER_READ_TIMEOUT", "server.read-timeout", reflect.TypeFor[time.Duration](), "Timeout"},
		{"rate_limit.limits.burst", "APP_RATE_LIMIT_LIMITS_BURST", "rate-limit.limits.burst", reflect.TypeFor[float64](), "Burst"},
		{"tags", "APP_TAGS", "tags", reflect.TypeFor[[]string](), "Tags"},
	}
	fields := s.Fields()
	if len(fields) != len(wants) {
		keys := make([]string, len(fields))
		for i, f := range fields {
			keys[i] = f.Key()
		}
		t.Fatalf("Fields() keys = %q, want %d fields", keys, len(wants))
	}
	root := reflect.TypeFor[schemaConfig]()
	for i, w := range wants {
		f := fields[i]
		if f.Key() != w.key || f.Env() != w.env || f.Flag() != w.flag || f.Type() != w.typ {
			t.Errorf("field %d = {%q %q %q %v}, want {%q %q %q %v}",
				i, f.Key(), f.Env(), f.Flag(), f.Type(), w.key, w.env, w.flag, w.typ)
		}
		if got := root.FieldByIndex(f.index); got.Name != w.goField || got.Type != w.typ {
			t.Errorf("field %q index %v reaches %s %v, want %s %v", w.key, f.index, got.Name, got.Type, w.goField, w.typ)
		}
	}
}

func TestNewSchemaAcceptsAStructValueAndNoPrefix(t *testing.T) {
	s, err := NewSchema(schemaServer{}, "")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	f, ok := s.Lookup("port")
	if !ok || f.Env() != "PORT" {
		t.Errorf("Lookup(port) = %q %v, want env PORT", f.Env(), ok)
	}
}

func TestSchemaFieldsReturnsACopy(t *testing.T) {
	s, err := NewSchema(&schemaServer{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	s.Fields()[0] = Field{}
	if got := s.Fields()[0].Key(); got != "host" {
		t.Errorf("Fields()[0].Key() after modifying a returned slice = %q, want host", got)
	}
}

func TestSchemaLookup(t *testing.T) {
	s, err := NewSchema(&schemaConfig{hidden: "unexported"}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	cases := []struct {
		name    string
		key     string
		wantKey string
	}{
		{name: "canonical key", key: "server.max_retries", wantKey: "server.max_retries"},
		{name: "kebab-case", key: "server.max-retries", wantKey: "server.max_retries"},
		{name: "camelCase", key: "server.maxRetries", wantKey: "server.max_retries"},
		{name: "upper-case", key: "SERVER.MAX_RETRIES", wantKey: "server.max_retries"},
		{name: "words run together", key: "server.maxretries", wantKey: "server.max_retries"},
		{name: "relaxed in every segment", key: "Rate-Limit.LIMITS.burst", wantKey: "rate_limit.limits.burst"},
		{name: "tag name, not field name", key: "server.read_timeout", wantKey: "server.read_timeout"},
		{name: "field name of a tagged field", key: "server.timeout"},
		{name: "dot replaced by underscore", key: "server_port"},
		{name: "dot inserted", key: "server.max.retries"},
		{name: "nested struct", key: "server"},
		{name: "skipped field", key: "skipped"},
		{name: "unexported field", key: "hidden"},
		{name: "unknown key", key: "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := s.Lookup(tc.key)
			if tc.wantKey == "" {
				if ok {
					t.Errorf("Lookup(%q) = %q, want no field", tc.key, f.Key())
				}
				return
			}
			if !ok || f.Key() != tc.wantKey {
				t.Errorf("Lookup(%q) = %q %v, want %q", tc.key, f.Key(), ok, tc.wantKey)
			}
		})
	}
}

type badPointer struct{ Port *int }

type badMap struct{ Labels map[int]string }

type badInterface struct{ Value any }

type badComplex struct{ Value complex128 }

type badNestedSlice struct{ Matrix [][]int }

type badStructSlice struct{ Servers []*schemaServer }

type badArray struct{ Pair [2]int }

type badTime struct{ At time.Time }

type badEmbedded struct {
	schemaServer
}

type badEmbeddedExported struct {
	Base
}

type Base struct{ Port int }

type badTag struct {
	Port int `config:"server.port"`
}

type badTagStart struct {
	Port int `config:"2port"`
}

type badEmptyNested struct {
	Inner struct {
		Skipped int `config:"-"`
	}
}

type badNestedPointer struct {
	Server struct{ Port *int }
}

func TestNewSchemaFieldErrors(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   string
	}{
		{name: "pointer field", target: badPointer{}, want: "field badPointer.Port: pointer fields are not supported"},
		{name: "map with a non-string key", target: badMap{}, want: "field badMap.Labels: type map[int]string is not supported: a map key is a string"},
		{name: "interface field", target: badInterface{}, want: "field badInterface.Value: type interface {} is not supported"},
		{name: "complex field", target: badComplex{}, want: "field badComplex.Value: type complex128 is not supported"},
		{name: "slice of slices", target: badNestedSlice{}, want: "field badNestedSlice.Matrix: type [][]int is not supported"},
		{name: "slice of struct pointers", target: badStructSlice{}, want: "field badStructSlice.Servers: type []*config.schemaServer is not supported"},
		{name: "array", target: badArray{}, want: "field badArray.Pair: type [2]int is not supported"},
		{name: "struct with no exported fields", target: badTime{}, want: "field badTime.At: type time.Time is not supported"},
		{name: "struct whose fields are all skipped", target: badEmptyNested{}, want: "field badEmptyNested.Inner: type struct { Skipped int \"config:\\\"-\\\"\" } is not supported"},
		{name: "embedded unexported struct", target: badEmbedded{}, want: "field badEmbedded.schemaServer: embedded fields are not supported"},
		{name: "embedded exported struct", target: badEmbeddedExported{}, want: "field badEmbeddedExported.Base: embedded fields are not supported"},
		{name: "dot in tag", target: badTag{}, want: `field badTag.Port: name "server.port" is not a valid key segment`},
		{name: "tag starting with a digit", target: badTagStart{}, want: `field badTagStart.Port: name "2port" is not a valid key segment`},
		{name: "unsupported field in a nested struct", target: badNestedPointer{}, want: "field badNestedPointer.Server.Port: pointer fields are not supported"},
		{name: "invalid prefix", target: schemaServer{}, want: `environment prefix "app" is not`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefix := "APP"
			if tc.name == "invalid prefix" {
				prefix = "app"
			}
			s, err := NewSchema(tc.target, prefix)
			if err == nil {
				t.Fatalf("NewSchema returned %d fields and no error, want %q", len(s.Fields()), tc.want)
			}
			if !errors.Is(err, ErrSchema) {
				t.Errorf("errors.Is(err, ErrSchema) = false for %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			if got := strings.Count(err.Error(), "\n") + 1; got != 1 {
				t.Errorf("error reports %d problems, want 1: %v", got, err)
			}
		})
	}
}

func TestNewSchemaRejectsANonStructTarget(t *testing.T) {
	cases := []struct {
		name   string
		target any
	}{
		{name: "nil", target: nil},
		{name: "int", target: 3},
		{name: "pointer to int", target: new(int)},
		{name: "map", target: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "APP")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema(%v) error = %v, want ErrSchema", tc.target, err)
			}
			if !strings.Contains(err.Error(), "not a struct or a pointer to a struct") {
				t.Errorf("error = %q, want it to say the target is not a struct", err)
			}
		})
	}
}

type keyCollision struct {
	MaxRetries int
	Retries    int `config:"max-retries"`
}

type envCollision struct {
	A struct {
		BC int `config:"b_c"`
	}
	AB struct {
		C int
	} `config:"a_b"`
}

type flagCollision struct {
	A struct {
		BC int `config:"b_c"`
		Bc int `config:"b-c"`
	}
}

type groupCollision struct {
	Server int
	Nested struct {
		Port int
	} `config:"server"`
}

type threeWayCollision struct {
	MaxRetries  int
	Max_Retries int
	Retries     int `config:"maxRetries"`
}

func TestNewSchemaCollisions(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   []string
	}{
		{
			name:   "keys matching when case, dash and underscore are ignored",
			target: keyCollision{},
			want: []string{
				`key "maxretries", ignoring case, "-" and "_", is shared by keyCollision.MaxRetries (key "max_retries"), keyCollision.Retries (key "max_retries")`,
				`environment variable "APP_MAX_RETRIES" is shared by keyCollision.MaxRetries (key "max_retries"), keyCollision.Retries (key "max_retries")`,
				`flag "max-retries" is shared by keyCollision.MaxRetries (key "max_retries"), keyCollision.Retries (key "max_retries")`,
			},
		},
		{
			name:   "environment variable derived from different keys",
			target: envCollision{},
			want: []string{
				`environment variable "APP_A_B_C" is shared by envCollision.A.BC (key "a.b_c"), envCollision.AB.C (key "a_b.c")`,
			},
		},
		{
			name:   "flag derived from keys that differ only in the separator",
			target: flagCollision{},
			want: []string{
				`key "a.bc", ignoring case, "-" and "_", is shared by flagCollision.A.BC (key "a.b_c"), flagCollision.A.Bc (key "a.b_c")`,
				`environment variable "APP_A_B_C" is shared by flagCollision.A.BC (key "a.b_c"), flagCollision.A.Bc (key "a.b_c")`,
				`flag "a.b-c" is shared by flagCollision.A.BC (key "a.b_c"), flagCollision.A.Bc (key "a.b_c")`,
			},
		},
		{
			name:   "key that is also the group of another",
			target: groupCollision{},
			want: []string{
				`groupCollision.Server (key "server") is a value and also the group of groupCollision.Nested.Port (key "server.port")`,
			},
		},
		{
			name:   "every field of a three-way collision is named in one problem",
			target: threeWayCollision{},
			want: []string{
				`key "maxretries", ignoring case, "-" and "_", is shared by threeWayCollision.MaxRetries (key "max_retries"), threeWayCollision.Max_Retries (key "max_retries"), threeWayCollision.Retries (key "max_retries")`,
				`environment variable "APP_MAX_RETRIES" is shared by threeWayCollision.MaxRetries (key "max_retries"), threeWayCollision.Max_Retries (key "max_retries"), threeWayCollision.Retries (key "max_retries")`,
				`flag "max-retries" is shared by threeWayCollision.MaxRetries (key "max_retries"), threeWayCollision.Max_Retries (key "max_retries"), threeWayCollision.Retries (key "max_retries")`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "APP")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema error = %v, want ErrSchema", err)
			}
			got := problems(err)
			want := make([]string, len(tc.want))
			for i, w := range tc.want {
				want[i] = "config: invalid schema: " + w
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

type everyProblem struct {
	Port    *int
	Labels  map[int]string
	Server  int
	Nested  struct{ Port int } `config:"server"`
	BadTag  int                `config:"a.b"`
	Retries int                `config:"max_retries"`
	Max     int                `config:"MaxRetries"`
	Value   complex64
	schemaServer
}

func TestNewSchemaReportsEveryProblemTogether(t *testing.T) {
	_, err := NewSchema(everyProblem{}, "bad")
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("NewSchema error = %v, want ErrSchema", err)
	}
	wants := []string{
		`environment prefix "bad"`,
		"field everyProblem.Port: pointer fields are not supported",
		"field everyProblem.Labels: type map[int]string is not supported: a map key is a string",
		`field everyProblem.BadTag: name "a.b" is not a valid key segment`,
		"field everyProblem.Value: type complex64 is not supported",
		"field everyProblem.schemaServer: embedded fields are not supported",
		`key "maxretries", ignoring case, "-" and "_", is shared by everyProblem.Retries (key "max_retries"), everyProblem.Max (key "max_retries")`,
		`environment variable "bad_MAX_RETRIES" is shared by`,
		`flag "max-retries" is shared by`,
		`everyProblem.Server (key "server") is a value and also the group of everyProblem.Nested.Port (key "server.port")`,
	}
	got := problems(err)
	if len(got) != len(wants) {
		t.Errorf("error reports %d problems, want %d:\n%s", len(got), len(wants), strings.Join(got, "\n"))
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error does not contain %q:\n%v", w, err)
		}
	}
}

// problems splits an error from NewSchema into one line per problem.
func problems(err error) []string {
	return strings.Split(err.Error(), "\n")
}

type docConfig struct {
	Verbose bool `doc:"Print every step."`
	Quiet   bool
	Server  struct {
		Port int `doc:"Port to listen on."`
	} `config:"httpServer" doc:"The HTTP server."`
	Rate struct {
		Burst float64
	}
}

func TestSchemaDoc(t *testing.T) {
	s, err := NewSchema(docConfig{}, "")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	fields := []struct{ key, want string }{
		{"verbose", "Print every step."},
		{"quiet", ""},
		{"http_server.port", "Port to listen on."},
		{"rate.burst", ""},
	}
	for _, tc := range fields {
		f, ok := s.Lookup(tc.key)
		if !ok {
			t.Fatalf("Lookup(%q) found no field", tc.key)
		}
		if got := f.Doc(); got != tc.want {
			t.Errorf("Lookup(%q).Doc() = %q, want %q", tc.key, got, tc.want)
		}
	}
	groups := []struct{ key, want string }{
		{"http_server", "The HTTP server."},
		{"HTTP-Server", "The HTTP server."},
		{"rate", ""},
		{"verbose", ""},
		{"http_server.port", ""},
		{"nope", ""},
	}
	for _, tc := range groups {
		if got := s.GroupDoc(tc.key); got != tc.want {
			t.Errorf("GroupDoc(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

type docOnSkipped struct {
	Port    int
	Skipped int `config:"-" doc:"Never shown."`
}

type docOnUnexported struct {
	Port   int
	hidden int `doc:"Never shown."`
}

type docEmptyOnSkipped struct {
	Port    int
	Skipped int `config:"-" doc:""`
}

func TestNewSchemaRejectsADocTagWithNoKey(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   string
	}{
		{name: "config:\"-\" field", target: docOnSkipped{}, want: `field docOnSkipped.Skipped: a doc tag on a field tagged config:"-" describes no key`},
		{name: "empty doc on a config:\"-\" field", target: docEmptyOnSkipped{}, want: `field docEmptyOnSkipped.Skipped: a doc tag on a field tagged config:"-" describes no key`},
		{name: "unexported field", target: docOnUnexported{hidden: 1}, want: "field docOnUnexported.hidden: a doc tag on an unexported field describes no key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema error = %v, want ErrSchema", err)
			}
			want := []string{"config: invalid schema: " + tc.want}
			if got := problems(err); !reflect.DeepEqual(got, want) {
				t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

type removedByValue struct {
	Server struct{ Port int }
}

func (removedByValue) RemovedKeys() map[string]string {
	return map[string]string{
		"legacy":           "drop it",
		"server.max_conns": "use server.port",
	}
}

type removedByPointer struct {
	Port int
}

func (*removedByPointer) RemovedKeys() map[string]string {
	return map[string]string{"oldPort": "use port"}
}

type removedNested struct {
	Inner removedByPointer
}

func TestSchemaRemoved(t *testing.T) {
	cases := []struct {
		name   string
		target any
		key    string
		want   string
	}{
		{name: "value receiver, value target", target: removedByValue{}, key: "legacy", want: "drop it"},
		{name: "value receiver, pointer target", target: &removedByValue{}, key: "legacy", want: "drop it"},
		{name: "pointer receiver, value target", target: removedByPointer{}, key: "old_port", want: "use port"},
		{name: "pointer receiver, pointer target", target: &removedByPointer{}, key: "old_port", want: "use port"},
		{name: "nil pointer target", target: (*removedByPointer)(nil), key: "old_port", want: "use port"},
		{name: "nested key", target: removedByValue{}, key: "server.max_conns", want: "use server.port"},
		{name: "relaxed in every segment", target: removedByValue{}, key: "Server.MAX-CONNS", want: "use server.port"},
		{name: "camelCase entry, kebab-case key", target: removedByPointer{}, key: "old-port", want: "use port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSchema(tc.target, "")
			if err != nil {
				t.Fatalf("NewSchema: %v", err)
			}
			hint, ok := s.Removed(tc.key)
			if !ok || hint != tc.want {
				t.Errorf("Removed(%q) = %q %v, want %q true", tc.key, hint, ok, tc.want)
			}
		})
	}
}

func TestSchemaRemovedFindsNothing(t *testing.T) {
	cases := []struct {
		name   string
		target any
		key    string
	}{
		{name: "live key", target: removedByValue{}, key: "server.port"},
		{name: "group of a removed key", target: removedByValue{}, key: "server"},
		{name: "below a removed key", target: removedByValue{}, key: "legacy.x"},
		{name: "dot replaced by underscore", target: removedByValue{}, key: "server_max_conns"},
		{name: "unknown key", target: removedByValue{}, key: "nope"},
		{name: "type without RemovedKeys", target: schemaServer{}, key: "legacy"},
		{name: "RemovedKeys of a nested struct", target: removedNested{}, key: "inner.old_port"},
		{name: "RemovedKeys of a nested struct, top-level key", target: removedNested{}, key: "old_port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSchema(tc.target, "")
			if err != nil {
				t.Fatalf("NewSchema: %v", err)
			}
			if hint, ok := s.Removed(tc.key); ok {
				t.Errorf("Removed(%q) = %q true, want no removed key", tc.key, hint)
			}
		})
	}
}

type removedLive struct {
	Server struct{ MaxRetries int }
}

func (removedLive) RemovedKeys() map[string]string {
	return map[string]string{"Server.max-retries": "it is live"}
}

type removedGroup struct {
	Server struct{ Port int }
}

func (removedGroup) RemovedKeys() map[string]string {
	return map[string]string{"SERVER": "it is a group"}
}

type removedCollide struct {
	Port int
}

func (removedCollide) RemovedKeys() map[string]string {
	return map[string]string{"old_port": "a", "oldPort": "b", "old-port": "c", "legacy": "d", "LEGACY": "e"}
}

type removedInvalid struct {
	Port int
}

func (removedInvalid) RemovedKeys() map[string]string {
	return map[string]string{
		"":          "empty",
		"a..b":      "empty segment",
		".a":        "leading dot",
		"a.":        "trailing dot",
		"a.2b":      "segment starting with a digit",
		"a b":       "space",
		"server.ok": "valid",
	}
}

func TestNewSchemaRejectsRemovedKeys(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   []string
	}{
		{
			name:   "live key",
			target: removedLive{},
			want:   []string{`removed key "Server.max-retries" of removedLive.RemovedKeys matches the key of removedLive.Server.MaxRetries (key "server.max_retries")`},
		},
		{
			name:   "group of a live key",
			target: &removedGroup{},
			want:   []string{`removed key "SERVER" of removedGroup.RemovedKeys matches the group of removedGroup.Server.Port (key "server.port")`},
		},
		{
			name:   "entries matching each other",
			target: removedCollide{},
			want: []string{
				`removed keys "LEGACY", "legacy" of removedCollide.RemovedKeys match each other, ignoring case, "-" and "_"`,
				`removed keys "old-port", "oldPort", "old_port" of removedCollide.RemovedKeys match each other, ignoring case, "-" and "_"`,
			},
		},
		{
			name:   "not a dotted key",
			target: removedInvalid{},
			want: []string{
				`removed key "" of removedInvalid.RemovedKeys is not a dotted key`,
				`removed key ".a" of removedInvalid.RemovedKeys is not a dotted key`,
				`removed key "a b" of removedInvalid.RemovedKeys is not a dotted key`,
				`removed key "a." of removedInvalid.RemovedKeys is not a dotted key`,
				`removed key "a..b" of removedInvalid.RemovedKeys is not a dotted key`,
				`removed key "a.2b" of removedInvalid.RemovedKeys is not a dotted key`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema error = %v, want ErrSchema", err)
			}
			got := problems(err)
			if len(got) != len(tc.want) {
				t.Fatalf("error reports %d problems, want %d:\n%s", len(got), len(tc.want), strings.Join(got, "\n"))
			}
			for i, w := range tc.want {
				if !strings.HasPrefix(got[i], "config: invalid schema: "+w) {
					t.Errorf("problem %d = %q, want it to start with %q", i, got[i], w)
				}
			}
		})
	}
}

type collMeta struct {
	Owner string
	Level int
}

type collProfile struct {
	Name  string            `doc:"Name of the profile."`
	Env   map[string]string `doc:"Variables of the profile."`
	Hooks []string
	Meta  collMeta `doc:"Ownership."`
}

type collTemplate struct {
	ID   string `config:"id"`
	Body string
}

type collConfig struct {
	Verbose   bool
	Profiles  []collProfile `merge:"Name" doc:"Named profiles."`
	Templates []collTemplate
	Labels    map[string]string `merge:"keys"`
	Paths     map[string][]string
	Timeouts  map[string]time.Duration
	Servers   map[string]schemaServer
	Nested    map[string]map[string]collTemplate
	Deep      map[string]map[string]int
	Owned     []collProfile `merge:"meta.owner"`
	Group     struct {
		Rules []collTemplate `merge:"id"`
	}
}

func TestNewSchemaCollections(t *testing.T) {
	s, err := NewSchema(collConfig{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	type want struct {
		key      string
		env      string
		kind     Kind
		merge    string
		elemKeys []string
	}
	profileKeys := []string{"name", "env", "hooks", "meta.owner", "meta.level"}
	templateKeys := []string{"id", "body"}
	wants := []want{
		{key: "verbose", env: "APP_VERBOSE", kind: KindValue},
		{key: "profiles", kind: KindList, merge: "name", elemKeys: profileKeys},
		{key: "templates", kind: KindList, elemKeys: templateKeys},
		{key: "labels", kind: KindMap, merge: "keys"},
		{key: "paths", kind: KindMap},
		{key: "timeouts", kind: KindMap},
		{key: "servers", kind: KindMap, elemKeys: []string{"host", "port", "max_retries", "read_timeout"}},
		{key: "nested", kind: KindMap, elemKeys: templateKeys},
		{key: "deep", kind: KindMap},
		{key: "owned", kind: KindList, merge: "meta.owner", elemKeys: profileKeys},
		{key: "group.rules", kind: KindList, merge: "id", elemKeys: templateKeys},
	}
	fields := s.Fields()
	if len(fields) != len(wants) {
		t.Fatalf("Fields() has %d fields, want %d", len(fields), len(wants))
	}
	root := reflect.TypeFor[collConfig]()
	for i, w := range wants {
		f := fields[i]
		if f.Key() != w.key || f.Env() != w.env || f.Kind() != w.kind || f.Merge() != w.merge {
			t.Errorf("field %d = {%q env %q %v merge %q}, want {%q env %q %v merge %q}",
				i, f.Key(), f.Env(), f.Kind(), f.Merge(), w.key, w.env, w.kind, w.merge)
		}
		if w.kind != KindValue && f.Flag() != "" {
			t.Errorf("field %q Flag() = %q, want none", w.key, f.Flag())
		}
		if got := root.FieldByIndex(f.index).Type; got != f.Type() {
			t.Errorf("field %q index %v reaches %v, want %v", w.key, f.index, got, f.Type())
		}
		if w.elemKeys == nil {
			if f.Elem() != nil {
				t.Errorf("field %q Elem() = %v, want nil", w.key, f.Elem())
			}
			continue
		}
		if f.Elem() == nil {
			t.Fatalf("field %q Elem() = nil, want a Schema", w.key)
		}
		var keys []string
		for _, ef := range f.Elem().Fields() {
			keys = append(keys, ef.Key())
			if ef.Env() != "" || ef.Flag() != "" {
				t.Errorf("element field %q of %q has env %q and flag %q, want neither", ef.Key(), w.key, ef.Env(), ef.Flag())
			}
		}
		if !reflect.DeepEqual(keys, w.elemKeys) {
			t.Errorf("field %q element keys = %q, want %q", w.key, keys, w.elemKeys)
		}
	}
}

func TestSchemaCollectionElement(t *testing.T) {
	s, err := NewSchema(collConfig{}, "APP")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	profiles, ok := s.Lookup("profiles")
	if !ok {
		t.Fatal("Lookup(profiles) found no field")
	}
	if got := profiles.Doc(); got != "Named profiles." {
		t.Errorf("profiles Doc() = %q, want %q", got, "Named profiles.")
	}
	elem := profiles.Elem()
	env, ok := elem.Lookup("ENV")
	if !ok {
		t.Fatal("element Lookup(ENV) found no field")
	}
	if env.Kind() != KindMap || env.Elem() != nil || env.Merge() != "" || env.Doc() != "Variables of the profile." {
		t.Errorf("element field env = {%v elem %v merge %q doc %q}, want a map of single values documented", env.Kind(), env.Elem(), env.Merge(), env.Doc())
	}
	owner, ok := elem.Lookup("meta.owner")
	if !ok {
		t.Fatal("element Lookup(meta.owner) found no field")
	}
	if got := reflect.TypeFor[collProfile]().FieldByIndex(owner.index).Name; got != "Owner" {
		t.Errorf("element field meta.owner index %v reaches %s, want Owner, relative to the element", owner.index, got)
	}
	if got := elem.GroupDoc("meta"); got != "Ownership." {
		t.Errorf("element GroupDoc(meta) = %q, want %q", got, "Ownership.")
	}
	for _, key := range []string{"profiles.name", "profiles[0].name", "labels.x", "nested.id"} {
		if f, ok := s.Lookup(key); ok {
			t.Errorf("Lookup(%q) = %q, want no field below a collection", key, f.Key())
		}
	}
	if _, ok := s.Lookup("group.rules"); !ok {
		t.Error("Lookup(group.rules) found no field")
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{KindValue: "value", KindList: "list", KindMap: "map"} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

type collKeysElement struct {
	Keys string
}

func TestNewSchemaMergeKeysOnAListNamesAnElementField(t *testing.T) {
	s, err := NewSchema(struct {
		L []collKeysElement `merge:"keys"`
	}{}, "")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	if got := s.Fields()[0].Merge(); got != "keys" {
		t.Errorf("Merge() = %q, want keys", got)
	}
}

type collRecursive struct {
	Name     string
	Children []collRecursive
}

type collRecursiveMap struct {
	Name string
	Sub  map[string]map[string]collRecursiveMap
}

type collEmbedded struct {
	collMeta
	Name string
}

func TestNewSchemaCollectionErrors(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   []string
	}{
		{
			name:   "non-string map key",
			target: struct{ M map[int]string }{},
			want:   []string{"field struct { M map[int]string }.M: type map[int]string is not supported: a map key is a string"},
		},
		{
			name:   "non-string key of a nested map",
			target: struct{ M map[string]map[bool]int }{},
			want:   []string{"field struct { M map[string]map[bool]int }.M: type map[string]map[bool]int is not supported: a map key is a string"},
		},
		{
			name:   "map of pointers",
			target: struct{ M map[string]*int }{},
			want:   []string{"field struct { M map[string]*int }.M: type map[string]*int is not supported"},
		},
		{
			name:   "map of struct pointers",
			target: struct{ M map[string]*collTemplate }{},
			want:   []string{"field struct { M map[string]*config.collTemplate }.M: type map[string]*config.collTemplate is not supported"},
		},
		{
			name:   "map of slices of structs",
			target: struct{ M map[string][]collTemplate }{},
			want:   []string{"field struct { M map[string][]config.collTemplate }.M: type map[string][]config.collTemplate is not supported"},
		},
		{
			name:   "map of slices of maps",
			target: struct{ M map[string][]map[string]int }{},
			want:   []string{"field struct { M map[string][]map[string]int }.M: type map[string][]map[string]int is not supported"},
		},
		{
			name:   "map of interfaces",
			target: struct{ M map[string]any }{},
			want:   []string{"field struct { M map[string]interface {} }.M: type map[string]interface {} is not supported"},
		},
		{
			name:   "slice of maps",
			target: struct{ L []map[string]string }{},
			want:   []string{"field struct { L []map[string]string }.L: a slice of maps is not supported"},
		},
		{
			name:   "slice of struct pointers",
			target: struct{ L []*collTemplate }{},
			want:   []string{"field struct { L []*config.collTemplate }.L: type []*config.collTemplate is not supported"},
		},
		{
			name:   "element with no keys",
			target: struct{ L []struct{ x int } }{L: []struct{ x int }{{x: 1}}},
			want:   []string{"field struct { L []struct { x int } }.L: type struct { x int } is not supported: an element struct needs at least one key"},
		},
		{
			name:   "map value with no keys",
			target: struct{ M map[string]time.Time }{},
			want:   []string{"field struct { M map[string]time.Time }.M: type time.Time is not supported: an element struct needs at least one key"},
		},
		{
			name:   "unsupported field of an element",
			target: struct{ L []struct{ P *int } }{},
			want:   []string{"field struct { L []struct { P *int } }.L[].P: pointer fields are not supported"},
		},
		{
			name:   "embedded field of an element",
			target: struct{ L []collEmbedded }{},
			want:   []string{"field struct { L []config.collEmbedded }.L[].collMeta: embedded fields are not supported; give the field a name"},
		},
		{
			name: "unsupported field of a map value nested in an element",
			target: struct {
				L []struct {
					M map[string]struct{ C complex64 }
				}
			}{},
			want: []string{"field struct { L []struct { M map[string]struct { C complex64 } } }.L[].M[].C: type complex64 is not supported"},
		},
		{
			name:   "colliding keys of an element, with no environment variable or flag to share",
			target: struct{ L []keyCollision }{},
			want: []string{
				`key "maxretries", ignoring case, "-" and "_", is shared by struct { L []config.keyCollision }.L[].MaxRetries (key "max_retries"), struct { L []config.keyCollision }.L[].Retries (key "max_retries")`,
			},
		},
		{
			name:   "a key of an element that is also a group",
			target: struct{ L []groupCollision }{},
			want: []string{
				`struct { L []config.groupCollision }.L[].Server (key "server") is a value and also the group of struct { L []config.groupCollision }.L[].Nested.Port (key "server.port")`,
			},
		},
		{
			name: "colliding collections, with no environment variable or flag to share",
			target: struct {
				A map[string]int `config:"a_b"`
				B []collTemplate `config:"a-b"`
			}{},
			want: []string{
				"key \"ab\", ignoring case, \"-\" and \"_\", is shared by struct { A map[string]int \"config:\\\"a_b\\\"\"; B []config.collTemplate \"config:\\\"a-b\\\"\" }.A (key \"a_b\"), struct { A map[string]int \"config:\\\"a_b\\\"\"; B []config.collTemplate \"config:\\\"a-b\\\"\" }.B (key \"a_b\")",
			},
		},
		{
			name:   "list holding its own element",
			target: collRecursive{},
			want:   []string{"field collRecursive.Children[].Children: type config.collRecursive is not supported: the element holds itself"},
		},
		{
			name:   "nested map holding its own element",
			target: collRecursiveMap{},
			want:   []string{"field collRecursiveMap.Sub[].Sub: type config.collRecursiveMap is not supported: the element holds itself"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "APP")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema error = %v, want ErrSchema", err)
			}
			want := make([]string, len(tc.want))
			for i, w := range tc.want {
				want[i] = "config: invalid schema: " + w
			}
			if got := problems(err); !reflect.DeepEqual(got, want) {
				t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

type mergeOnValue struct {
	Port int `merge:"keys"`
}

type mergeOnScalarSlice struct {
	Tags []string `merge:"keys"`
}

type mergeOnGroup struct {
	Server struct{ Port int } `merge:"keys"`
}

type mergeOnSkipped struct {
	Port    int
	Skipped map[string]int `config:"-" merge:"keys"`
}

type mergeOnUnexported struct {
	Port   int
	hidden map[string]int `merge:"keys"`
}

type mergeMapByName struct {
	Labels map[string]string `merge:"name"`
}

type mergeMapEmpty struct {
	Labels map[string]string `merge:""`
}

type mergeListUnknown struct {
	Profiles []collProfile `merge:"nope"`
}

type mergeListEmpty struct {
	Profiles []collProfile `merge:""`
}

type mergeListInt struct {
	Owned []collProfile `merge:"meta.level"`
}

type mergeListMap struct {
	Profiles []collProfile `merge:"env"`
}

type mergeListScalarSlice struct {
	Profiles []collProfile `merge:"hooks"`
}

type mergeListGroup struct {
	Profiles []collProfile `merge:"meta"`
}

type mergeListKeys struct {
	Profiles []collProfile `merge:"keys"`
}

type mergeInElementMap struct {
	Profiles []struct {
		Name string
		Env  map[string]string `merge:"keys"`
	}
}

type mergeInElementList struct {
	Servers map[string]struct {
		Routes []collTemplate `merge:"id"`
	}
}

type mergeInElementValue struct {
	Profiles []struct {
		Name string `merge:"name"`
	}
}

func TestNewSchemaMergeTagErrors(t *testing.T) {
	cases := []struct {
		name   string
		target any
		want   string
	}{
		{name: "single value", target: mergeOnValue{}, want: "field mergeOnValue.Port: a merge tag is only allowed on a map or a slice of structs"},
		{name: "slice of single values", target: mergeOnScalarSlice{}, want: "field mergeOnScalarSlice.Tags: a merge tag is only allowed on a map or a slice of structs"},
		{name: "nested struct", target: mergeOnGroup{}, want: "field mergeOnGroup.Server: a merge tag is only allowed on a map or a slice of structs"},
		{name: "config:\"-\" field", target: mergeOnSkipped{}, want: `field mergeOnSkipped.Skipped: a merge tag on a field tagged config:"-" describes no key`},
		{name: "unexported field", target: mergeOnUnexported{hidden: map[string]int{}}, want: "field mergeOnUnexported.hidden: a merge tag on an unexported field describes no key"},
		{name: "map merged by an element key", target: mergeMapByName{}, want: `field mergeMapByName.Labels: merge "name" on a map is not supported; a map takes merge:"keys"`},
		{name: "map with an empty merge tag", target: mergeMapEmpty{}, want: `field mergeMapEmpty.Labels: merge "" on a map is not supported; a map takes merge:"keys"`},
		{name: "list merged by an unknown key", target: mergeListUnknown{}, want: `field mergeListUnknown.Profiles: merge "nope" names no string field of the element config.collProfile`},
		{name: "list with an empty merge tag", target: mergeListEmpty{}, want: `field mergeListEmpty.Profiles: merge "" names no string field of the element config.collProfile`},
		{name: "list merged by an int field", target: mergeListInt{}, want: `field mergeListInt.Owned: merge "meta.level" names no string field of the element config.collProfile`},
		{name: "list merged by a map field", target: mergeListMap{}, want: `field mergeListMap.Profiles: merge "env" names no string field of the element config.collProfile`},
		{name: "list merged by a slice field", target: mergeListScalarSlice{}, want: `field mergeListScalarSlice.Profiles: merge "hooks" names no string field of the element config.collProfile`},
		{name: "list merged by a group", target: mergeListGroup{}, want: `field mergeListGroup.Profiles: merge "meta" names no string field of the element config.collProfile`},
		{name: "list merged by keys, with no keys field", target: mergeListKeys{}, want: `field mergeListKeys.Profiles: merge "keys" names no string field of the element config.collProfile`},
		{name: "map in a list element", target: mergeInElementMap{}, want: "field mergeInElementMap.Profiles[].Env: a merge tag is not allowed on a field of a collection element"},
		{name: "list in a map value", target: mergeInElementList{}, want: "field mergeInElementList.Servers[].Routes: a merge tag is not allowed on a field of a collection element"},
		{name: "single value in a list element", target: mergeInElementValue{}, want: "field mergeInElementValue.Profiles[].Name: a merge tag is only allowed on a map or a slice of structs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSchema(tc.target, "")
			if !errors.Is(err, ErrSchema) {
				t.Fatalf("NewSchema error = %v, want ErrSchema", err)
			}
			want := []string{"config: invalid schema: " + tc.want}
			if got := problems(err); !reflect.DeepEqual(got, want) {
				t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}
