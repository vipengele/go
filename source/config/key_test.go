package config

import "testing"

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
