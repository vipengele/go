package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "lower-case word is unchanged", in: "port", want: "port"},
		{name: "snake_case is unchanged", in: "max_retries", want: "max_retries"},
		{name: "kebab-case becomes snake_case", in: "max-retries", want: "max_retries"},
		{name: "camelCase splits before the upper-case letter", in: "maxRetries", want: "max_retries"},
		{name: "PascalCase splits before each word", in: "MaxRetries", want: "max_retries"},
		{name: "acronym run ends before the last upper-case letter", in: "HTTPServer", want: "http_server"},
		{name: "trailing acronym stays one word", in: "ServerURL", want: "server_url"},
		{name: "all upper-case is one word", in: "ID", want: "id"},
		{name: "digit then upper-case splits", in: "Key2Value", want: "key2_value"},
		{name: "trailing digit stays in the word", in: "APIKey2", want: "api_key2"},
		{name: "upper-case after a separator adds no second separator", in: "Max_Retries", want: "max_retries"},
		{name: "consecutive separators collapse", in: "max__-retries", want: "max_retries"},
		{name: "trailing separator is dropped", in: "port_", want: "port"},
		{name: "leading separator is dropped", in: "_port", want: "port"},
		{name: "leading separators before an upper-case letter are dropped", in: "-_Port", want: "port"},
		{name: "SCREAMING_SNAKE becomes snake_case", in: "MAX_RETRIES", want: "max_retries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snakeCase(tc.in); got != tc.want {
				t.Errorf("snakeCase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMatchKey(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{name: "case is ignored", a: "server.max_retries", b: "SERVER.MAX_RETRIES", same: true},
		{name: "dash is ignored", a: "server.max_retries", b: "server.max-retries", same: true},
		{name: "underscore is ignored", a: "server.max_retries", b: "server.maxretries", same: true},
		{name: "camelCase matches snake_case", a: "server.max_retries", b: "Server.MaxRetries", same: true},
		{name: "dot is never ignored", a: "server.max_retries", b: "server.max.retries", same: false},
		{name: "underscore does not stand for a dot", a: "server.port", b: "server_port", same: false},
		{name: "different words differ", a: "server.port", b: "server.host", same: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchKey(tc.a) == matchKey(tc.b); got != tc.same {
				t.Errorf("matchKey(%q) == matchKey(%q) is %v, want %v", tc.a, tc.b, got, tc.same)
			}
		})
	}
}

func TestEnvName(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{name: "prefix and upper-cased key", prefix: "APP", key: "port", want: "APP_PORT"},
		{name: "dots become underscores", prefix: "APP", key: "a.b.max_retries", want: "APP_A_B_MAX_RETRIES"},
		{name: "no prefix is the key alone", prefix: "", key: "server.port", want: "SERVER_PORT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := envName(tc.prefix, tc.key); got != tc.want {
				t.Errorf("envName(%q, %q) = %q, want %q", tc.prefix, tc.key, got, tc.want)
			}
		})
	}
}

func TestFlagName(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{name: "single word", key: "port", want: "port"},
		{name: "underscores become dashes", key: "max_retries", want: "max-retries"},
		{name: "dots are kept", key: "a.b.max_retries", want: "a.b.max-retries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := flagName(tc.key); got != tc.want {
				t.Errorf("flagName(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "lower-case word", in: "port", want: true},
		{name: "mixed case with digits, dash and underscore", in: "Max-Retries_2", want: true},
		{name: "empty", in: "", want: false},
		{name: "starts with a digit", in: "2fa", want: false},
		{name: "starts with an underscore", in: "_port", want: false},
		{name: "holds a dot", in: "server.port", want: false},
		{name: "holds a comma", in: "port,omitempty", want: false},
		{name: "holds a space", in: "max retries", want: false},
		{name: "non-ASCII letter", in: "größe", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validName(tc.in); got != tc.want {
				t.Errorf("validName(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidPrefix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "empty", in: "", want: true},
		{name: "upper-case word", in: "APP", want: true},
		{name: "digits and inner underscore", in: "MY_APP2", want: true},
		{name: "lower-case", in: "app", want: false},
		{name: "starts with a digit", in: "2APP", want: false},
		{name: "ends with an underscore", in: "APP_", want: false},
		{name: "holds a dash", in: "MY-APP", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validPrefix(tc.in); got != tc.want {
				t.Errorf("validPrefix(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCharacterClasses(t *testing.T) {
	cases := []struct {
		c                           byte
		upper, lower, digit, letter bool
	}{
		{c: '/'},
		{c: '0', digit: true},
		{c: '9', digit: true},
		{c: ':'},
		{c: '@'},
		{c: 'A', upper: true, letter: true},
		{c: 'Z', upper: true, letter: true},
		{c: '['},
		{c: '`'},
		{c: 'a', lower: true, letter: true},
		{c: 'z', lower: true, letter: true},
		{c: '{'},
	}
	for _, tc := range cases {
		t.Run(string(tc.c), func(t *testing.T) {
			if got := isUpper(tc.c); got != tc.upper {
				t.Errorf("isUpper(%q) = %v, want %v", tc.c, got, tc.upper)
			}
			if got := isLower(tc.c); got != tc.lower {
				t.Errorf("isLower(%q) = %v, want %v", tc.c, got, tc.lower)
			}
			if got := isDigit(tc.c); got != tc.digit {
				t.Errorf("isDigit(%q) = %v, want %v", tc.c, got, tc.digit)
			}
			if got := isLetter(tc.c); got != tc.letter {
				t.Errorf("isLetter(%q) = %v, want %v", tc.c, got, tc.letter)
			}
		})
	}
}

func TestPathRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		steps []pathStep
		want  string
	}{
		{name: "key alone", steps: []pathStep{keyStep("verbose")}, want: "verbose"},
		{name: "dotted key", steps: []pathStep{keyStep("server.max_retries")}, want: "server.max_retries"},
		{name: "list element field and map entry", steps: []pathStep{keyStep("profiles"), indexStep(2), keyStep("env"), mapKeyStep("FOO")}, want: `profiles[2].env["FOO"]`},
		{name: "index zero", steps: []pathStep{keyStep("profiles"), indexStep(0)}, want: "profiles[0]"},
		{name: "large index", steps: []pathStep{keyStep("profiles"), indexStep(1234)}, want: "profiles[1234]"},
		{name: "nested maps", steps: []pathStep{keyStep("group.nested"), mapKeyStep("a"), mapKeyStep("b"), keyStep("meta.owner")}, want: `group.nested["a"]["b"].meta.owner`},
		{name: "empty map key", steps: []pathStep{keyStep("labels"), mapKeyStep("")}, want: `labels[""]`},
		{name: "map key holding a quote", steps: []pathStep{keyStep("labels"), mapKeyStep(`say "hi"`)}, want: `labels["say \"hi\""]`},
		{name: "map key holding a bracket", steps: []pathStep{keyStep("labels"), mapKeyStep("a]b[")}, want: `labels["a]b["]`},
		{name: "map key holding a backslash", steps: []pathStep{keyStep("labels"), mapKeyStep(`C:\dir`)}, want: `labels["C:\\dir"]`},
		{name: "map key holding dots", steps: []pathStep{keyStep("labels"), mapKeyStep("example.com.")}, want: `labels["example.com."]`},
		{name: "map key holding every special byte", steps: []pathStep{keyStep("labels"), mapKeyStep(`"].\[`), keyStep("x")}, want: `labels["\"].\\["].x`},
		{name: "map key kept verbatim, not normalised", steps: []pathStep{keyStep("labels"), mapKeyStep("Max-Retries_X")}, want: `labels["Max-Retries_X"]`},
		{name: "map key with a newline and non-ASCII", steps: []pathStep{keyStep("labels"), mapKeyStep("a\nb\u00e9")}, want: `labels["a\nbé"]`},
		{name: "map key of invalid UTF-8", steps: []pathStep{keyStep("labels"), mapKeyStep("\xff")}, want: `labels["\xff"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatPath(tc.steps)
			if got != tc.want {
				t.Errorf("formatPath = %s, want %s", got, tc.want)
			}
			steps, err := parsePath(got)
			if err != nil {
				t.Fatalf("parsePath(%s): %v", got, err)
			}
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Errorf("parsePath(%s) = %#v, want %#v", got, steps, tc.steps)
			}
		})
	}
}

func TestParsePathJoinsKeySegments(t *testing.T) {
	steps, err := parsePath(`a.b[1].c.d["k"].e`)
	if err != nil {
		t.Fatalf("parsePath: %v", err)
	}
	want := []pathStep{keyStep("a.b"), indexStep(1), keyStep("c.d"), mapKeyStep("k"), keyStep("e")}
	if !reflect.DeepEqual(steps, want) {
		t.Errorf("parsePath = %#v, want %#v", steps, want)
	}
}

func TestParsePathErrors(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: "", want: "a key segment is missing at its end"},
		{name: "trailing dot", path: "a.", want: "a key segment is missing at its end"},
		{name: "trailing dot after a bracket", path: "a[0].", want: "a key segment is missing at its end"},
		{name: "leading bracket", path: "[0]", want: `"" at offset 0 is not a key segment`},
		{name: "leading dot", path: ".a", want: `"" at offset 0 is not a key segment`},
		{name: "empty segment", path: "a..b", want: `"" at offset 2 is not a key segment`},
		{name: "invalid segment", path: "a.2b", want: `"2b" at offset 2 is not a key segment`},
		{name: "segment holding a space", path: "a b", want: `"a b" at offset 0 is not a key segment`},
		{name: "bracket left open", path: "a[", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "empty brackets", path: "a[]", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "negative index", path: "a[-1]", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "unquoted map key", path: "a[FOO]", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "single-quoted map key", path: "a['x']", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "leading zero", path: "a[01]", want: "the index 01 has a leading zero"},
		{name: "index not closed", path: "a[1", want: `the index 1 is not followed by "]"`},
		{name: "index followed by text", path: "a[1x]", want: `the index 1 is not followed by "]"`},
		{name: "index out of range", path: "a[99999999999999999999]", want: "the index 99999999999999999999: "},
		{name: "map key not closed", path: `a["x"`, want: `the map key "x" is not followed by "]"`},
		{name: "map key followed by text", path: `a["x"y]`, want: `the map key "x" is not followed by "]"`},
		{name: "unterminated map key", path: `a["x]`, want: "a map key is not a double-quoted string"},
		{name: "bad escape in a map key", path: `a["\q"]`, want: "a map key is not a double-quoted string"},
		{name: "text after a bracket", path: "a[0]b", want: `'b' at offset 4 follows a "]"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, err := parsePath(tc.path)
			if err == nil {
				t.Fatalf("parsePath(%q) = %#v, want an error", tc.path, steps)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseBracketErrorSpansNoBytes(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want string
	}{
		{name: "unterminated map key", s: `["x]`, want: "a map key is not a double-quoted string"},
		{name: "map key not closed", s: `["x"y]`, want: `the map key "x" is not followed by "]"`},
		{name: "neither index nor map key", s: "[x]", want: "a bracket holds neither an index nor a double-quoted map key"},
		{name: "leading zero", s: "[01]", want: "the index 01 has a leading zero"},
		{name: "index not closed", s: "[1x]", want: `the index 1 is not followed by "]"`},
		{name: "index out of range", s: "[99999999999999999999]", want: "the index 99999999999999999999: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, n, err := parseBracket(tc.s)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseBracket(%q) error = %v, want it to contain %q", tc.s, err, tc.want)
			}
			if n != 0 || step != (pathStep{}) {
				t.Errorf("parseBracket(%q) = %#v, %d alongside its error, want the zero step and 0", tc.s, step, n)
			}
		})
	}
}
