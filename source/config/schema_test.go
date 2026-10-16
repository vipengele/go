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

type badMap struct{ Labels map[string]string }

type badInterface struct{ Value any }

type badComplex struct{ Value complex128 }

type badNestedSlice struct{ Matrix [][]int }

type badStructSlice struct{ Servers []schemaServer }

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
		{name: "map field", target: badMap{}, want: "field badMap.Labels: maps are not supported"},
		{name: "interface field", target: badInterface{}, want: "field badInterface.Value: type interface {} is not supported"},
		{name: "complex field", target: badComplex{}, want: "field badComplex.Value: type complex128 is not supported"},
		{name: "slice of slices", target: badNestedSlice{}, want: "field badNestedSlice.Matrix: type [][]int is not supported"},
		{name: "slice of structs", target: badStructSlice{}, want: "field badStructSlice.Servers: type []config.schemaServer is not supported"},
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
	Labels  map[string]string
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
		"field everyProblem.Labels: maps are not supported",
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
