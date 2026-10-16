package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type validServer struct {
	Port int
}

// validConfig validates itself with check, counting each call in calls when it is set. Neither
// field is exported, so neither is a key.
type validConfig struct {
	Port   int
	Server validServer
	Tags   []string
	check  func(validConfig) error
	calls  *int
}

func (c validConfig) Validate() error {
	if c.calls != nil {
		*c.calls++
	}
	if c.check == nil {
		return nil
	}
	return c.check(c)
}

// pointerValidConfig implements Validator on its pointer only.
type pointerValidConfig struct {
	Port int
}

func (c *pointerValidConfig) Validate() error {
	if c.Port > 1000 {
		return &FieldError{Key: "port", Err: errors.New("must be at most 1000")}
	}
	return nil
}

func validDefaults(check func(validConfig) error) validConfig {
	return validConfig{Port: 1, Server: validServer{Port: 80}, Tags: []string{"a"}, check: check}
}

// failWith returns a check that always returns err.
func failWith(err error) func(validConfig) error {
	return func(validConfig) error { return err }
}

// loadInvalid runs Load expecting an error and the zero Result, and returns the error.
func loadInvalid(t *testing.T, defaults validConfig, providers ...Provider) error {
	t.Helper()
	res, err := Load(defaults, providers...)
	if err == nil {
		t.Fatal("Load: no error")
	}
	if !reflect.DeepEqual(res, Result[validConfig]{}) {
		t.Errorf("Result = %+v, want the zero Result", res)
	}
	return err
}

func TestLoadValidateNamesTheOrigin(t *testing.T) {
	file := writeFile(t, "config.yaml", "server:\n  port: 8080\n")
	tests := []struct {
		name      string
		providers []Provider
		value     string
		origin    string
	}{
		{name: "file", providers: []Provider{File(file)}, value: "8080", origin: "config.yaml:2"},
		{
			name:      "env",
			providers: []Provider{File(file), Env("APP", mapLookup(map[string]string{"APP_SERVER_PORT": "9090"}))},
			value:     "9090",
			origin:    "env APP_SERVER_PORT",
		},
		{
			name:      "flag",
			providers: []Provider{File(file), Flags(map[string]string{"server.port": "7070"})},
			value:     "7070",
			origin:    "flag --server.port",
		},
		{name: "default", value: "80", origin: "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := &FieldError{Key: "Server.Port", Err: errors.New("must be below 1024")}
			err := loadInvalid(t, validDefaults(failWith(orig)), tt.providers...)

			want := fmt.Sprintf("server.port: must be below 1024: %q from %s", tt.value, tt.origin)
			if got := err.Error(); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("errors.As(%v, *FieldError) = false", err)
			}
			wantFE := FieldError{Key: "server.port", Value: tt.value, Origin: tt.origin, Err: orig.Err}
			if *fe != wantFE {
				t.Errorf("FieldError = %+v, want %+v", *fe, wantFE)
			}
			if !errors.Is(err, orig) {
				t.Error("errors.Is(err, the FieldError Validate returned) = false")
			}
			if orig.Key != "Server.Port" || orig.Value != "" || orig.Origin != "" {
				t.Errorf("the FieldError Validate returned was changed: %+v", *orig)
			}
		})
	}
}

func TestLoadValidateWrappedFieldError(t *testing.T) {
	sentinel := errors.New("out of range")
	orig := &FieldError{Key: "server.port", Err: sentinel}
	err := loadInvalid(t, validDefaults(failWith(fmt.Errorf("checking server: %w", orig))))

	if got, want := err.Error(), `checking server: server.port: out of range: "80" from default`; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Origin != "default" {
		t.Errorf("errors.As found %+v, want the annotated copy", fe)
	}
	if !errors.Is(err, sentinel) || !errors.Is(err, orig) {
		t.Error("the wrapped chain is not reachable through errors.Is")
	}
}

func TestLoadValidateJoinedErrors(t *testing.T) {
	file := writeFile(t, "config.yaml", "port: 5\n")
	plain := errors.New("tags and port disagree")
	check := func(c validConfig) error {
		return errors.Join(
			&FieldError{Key: "port", Err: errors.New("bad")},
			plain,
			&FieldError{Key: "server.port", Err: errors.New("bad")},
			&FieldError{Key: "tags", Err: errors.New("too few")},
		)
	}
	err := loadInvalid(t, validDefaults(check), File(file))

	want := []string{
		`port: bad: "5" from config.yaml:1`,
		"tags and port disagree",
		`server.port: bad: "80" from default`,
		`tags: too few: "a" from default`,
	}
	if got := strings.Split(err.Error(), "\n"); !reflect.DeepEqual(got, want) {
		t.Errorf("error =\n%s\nwant\n%s", err, strings.Join(want, "\n"))
	}
	if !errors.Is(err, plain) {
		t.Error("errors.Is(err, plain) = false")
	}
}

func TestLoadValidatePrefixTexts(t *testing.T) {
	check := func(c validConfig) error {
		return errors.Join(
			&FieldError{Key: "server.port", Err: errors.New("bad")},
			&FieldError{Key: "server.port", Err: errors.New("bad value")},
		)
	}
	err := loadInvalid(t, validDefaults(check))

	want := `server.port: bad: "80" from default` + "\n" + `server.port: bad value: "80" from default`
	if got := err.Error(); got != want {
		t.Errorf("error =\n%s\nwant\n%s", got, want)
	}
}

func TestLoadValidateUnmatchedKeyPassesThrough(t *testing.T) {
	orig := &FieldError{Key: "server", Err: errors.New("bad")}
	err := loadInvalid(t, validDefaults(failWith(orig)))

	if err != error(orig) {
		t.Errorf("error = %#v, want the FieldError Validate returned", err)
	}
	if got, want := err.Error(), "server: bad"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestLoadValidatePlainError(t *testing.T) {
	plain := errors.New("inconsistent")
	err := loadInvalid(t, validDefaults(failWith(plain)))
	if err != plain {
		t.Errorf("error = %v, want %v as Validate returned it", err, plain)
	}
}

func TestLoadValidateMixedJoinKeepsUnmatched(t *testing.T) {
	check := failWith(errors.Join(
		&FieldError{Key: "nope", Err: errors.New("bad")},
		&FieldError{Key: "port", Err: errors.New("bad")},
	))
	err := loadInvalid(t, validDefaults(check))
	want := "nope: bad\n" + `port: bad: "1" from default`
	if got := err.Error(); got != want {
		t.Errorf("error =\n%s\nwant\n%s", got, want)
	}
}

func TestLoadValidateDoesNotRunWhenBindingFails(t *testing.T) {
	calls := 0
	defaults := validDefaults(nil)
	defaults.calls = &calls
	tests := []struct {
		name      string
		providers []Provider
	}{
		{name: "conversion", providers: []Provider{Flags(map[string]string{"port": "abc"})}},
		{name: "provider", providers: []Provider{Flags(map[string]string{"nope": "1"})}},
		{name: "nil provider", providers: []Provider{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = 0
			_ = loadInvalid(t, defaults, tt.providers...)
			if calls != 0 {
				t.Errorf("Validate ran %d times, want 0", calls)
			}
		})
	}
}

func TestLoadValidateRunsOnceAndPasses(t *testing.T) {
	calls := 0
	defaults := validDefaults(nil)
	defaults.calls = &calls
	res, err := Load(defaults, Flags(map[string]string{"port": "3"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if calls != 1 {
		t.Errorf("Validate ran %d times, want 1", calls)
	}
	if got := res.Value().Port; got != 3 {
		t.Errorf("Port = %d, want 3", got)
	}
}

func TestLoadValidatePointerReceiver(t *testing.T) {
	if _, err := Load(pointerValidConfig{Port: 5}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	res, err := Load(pointerValidConfig{Port: 5}, Flags(map[string]string{"port": "2000"}))
	if got, want := fmt.Sprint(err), `port: must be at most 1000: "2000" from flag --port`; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(res, Result[pointerValidConfig]{}) {
		t.Errorf("Result = %+v, want the zero Result", res)
	}
}

func TestLoadValidateNestedStructIsNotAsked(t *testing.T) {
	type outer struct {
		Inner pointerValidConfig
	}
	if _, err := Load(outer{Inner: pointerValidConfig{Port: 2000}}); err != nil {
		t.Errorf("Load: %v, want the nested Validate not to run", err)
	}
}

func TestFieldErrorError(t *testing.T) {
	bad := errors.New("bad")
	tests := []struct {
		name string
		err  FieldError
		want string
	}{
		{name: "annotated", err: FieldError{Key: "port", Value: "8", Origin: "flag --port", Err: bad}, want: `port: bad: "8" from flag --port`},
		{name: "empty value", err: FieldError{Key: "name", Origin: "default", Err: bad}, want: `name: bad: "" from default`},
		{name: "value only", err: FieldError{Key: "port", Value: "8", Err: bad}, want: `port: bad: "8"`},
		{name: "key only", err: FieldError{Key: "port", Err: bad}, want: "port: bad"},
		{name: "no key", err: FieldError{Err: bad}, want: "bad"},
		{name: "no error", err: FieldError{Key: "port"}, want: "port: invalid value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFieldErrorUnwrap(t *testing.T) {
	bad := errors.New("bad")
	if got := (&FieldError{Key: "port", Err: bad}).Unwrap(); got != bad {
		t.Errorf("Unwrap() = %v, want %v", got, bad)
	}
}
