package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// plistEscapes holds every character xml.EscapeText must escape in an
// element's text.
const plistEscapes = `& < > " '`

// launchdPlistCase is one golden of renderPlist with the top-level keys its
// plist holds, in order.
type launchdPlistCase struct {
	name string
	cfg  Config
	logs launchdLogs
	keys []string
}

// plistKeys returns the top-level keys of a rendered plist: the fixed ones,
// with each of extra inserted at its place in the fixed order.
func plistKeys(extra ...string) []string {
	order := []string{
		"Label", "ProgramArguments", "EnvironmentVariables", "WorkingDirectory", "UserName",
		"RunAtLoad", "KeepAlive", "ThrottleInterval", "StandardOutPath", "StandardErrorPath",
	}
	optional := []string{"EnvironmentVariables", "WorkingDirectory", "UserName"}
	var keys []string
	for _, k := range order {
		if !slices.Contains(optional, k) || slices.Contains(extra, k) {
			keys = append(keys, k)
		}
	}
	return keys
}

// escapingPlistConfig carries every character of plistEscapes in every string
// field renderPlist writes, an Env key included, and in Description, which it
// does not write.
func escapingPlistConfig() Config {
	return Config{
		Name:        `esc&<>"'`,
		Description: `desc & <b> "q" 'a'`,
		Binary:      `/opt/a&b/<bin>"'`,
		Args:        []string{plistEscapes, "", `--x="1"`, `it's`},
		Env:         map[string]string{`K&<>"'`: plistEscapes, "PLAIN": `<v>`},
		WorkingDir:  `/srv/&<>"'`,
		User:        `u&<>"'`,
	}
}

func launchdPlistCases() []launchdPlistCase {
	return []launchdPlistCase{
		{
			name: "agent-default",
			cfg: Config{
				Name:        "demo",
				Description: "Demo agent",
				Binary:      "/Users/me/bin/demo",
				Scope:       ScopeUser,
			},
			logs: defaultLaunchdLogs(ScopeUser, "demo", "/Users/me"),
			keys: plistKeys(),
		},
		{
			name: "daemon-default",
			cfg: Config{
				Name:        "demo",
				Description: "Demo daemon",
				Binary:      "/usr/local/bin/demo",
				Args:        []string{"serve"},
				User:        "_demo",
			},
			logs: defaultLaunchdLogs(ScopeSystem, "demo", "/Users/me"),
			keys: plistKeys("UserName"),
		},
		{
			name: "env-args-workingdir",
			cfg: Config{
				Name:       "org.example.demo",
				Binary:     "/usr/local/bin/demo",
				Args:       []string{"serve", "--port=8080", ""},
				Env:        map[string]string{"LOG_LEVEL": "debug", "APP_MODE": "prod", "_FEATURE": "on"},
				WorkingDir: "/var/lib/demo",
			},
			logs: defaultLaunchdLogs(ScopeSystem, "org.example.demo", ""),
			keys: plistKeys("EnvironmentVariables", "WorkingDirectory"),
		},
		{
			name: "escaping",
			cfg:  escapingPlistConfig(),
			logs: launchdLogs{stdout: `/var/log/&<>"'.out`, stderr: `/var/log/&<>"'.err`},
			keys: plistKeys("EnvironmentVariables", "WorkingDirectory", "UserName"),
		},
		{
			name: "custom-logs",
			cfg: Config{
				Name:   "demo",
				Binary: "/Users/me/bin/demo",
				Scope:  ScopeUser,
			},
			logs: launchdLogs{stdout: "/Users/me/logs/demo.out"}.
				withDefaults(defaultLaunchdLogs(ScopeUser, "demo", "/Users/me")),
			keys: plistKeys(),
		},
	}
}

func TestRenderPlistGolden(t *testing.T) {
	for _, tt := range launchdPlistCases() {
		t.Run(tt.name, func(t *testing.T) {
			got := renderPlist(tt.cfg, tt.logs)
			checkGolden(t, "launchd", tt.name, got)
			var desc bytes.Buffer
			if err := xml.EscapeText(&desc, []byte(tt.cfg.Description)); err != nil {
				t.Fatal(err)
			}
			if tt.cfg.Description != "" && bytes.Contains(got, desc.Bytes()) {
				t.Errorf("plist renders the Description %q:\n%s", tt.cfg.Description, got)
			}
		})
	}
}

func TestRenderPlistIsDeterministic(t *testing.T) {
	for _, tt := range launchdPlistCases() {
		t.Run(tt.name, func(t *testing.T) {
			first := renderPlist(tt.cfg, tt.logs)
			for range 20 {
				if got := renderPlist(tt.cfg, tt.logs); !bytes.Equal(got, first) {
					t.Fatalf("renderPlist differs between calls:\n%s\n---\n%s", first, got)
				}
			}
		})
	}
}

// TestLaunchdGoldenStructure walks every golden of a case with xml.Decoder,
// so that a golden rewritten with -update is still a well-formed plist.
func TestLaunchdGoldenStructure(t *testing.T) {
	for _, tt := range launchdPlistCases() {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "launchd", tt.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			keys, _, err := decodePlist(data)
			if err != nil {
				t.Fatalf("decoding the golden: %v\n%s", err, data)
			}
			if !slices.Equal(keys, tt.keys) {
				t.Errorf("top-level keys = %q, want %q", keys, tt.keys)
			}
		})
	}
}

func TestRenderPlistHeader(t *testing.T) {
	got := string(renderPlist(Config{Name: "demo", Binary: "/bin/demo"}, defaultLaunchdLogs(ScopeSystem, "demo", "")))
	want := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n" +
		`<plist version="1.0">` + "\n<dict>\n"
	if !strings.HasPrefix(got, want) {
		t.Errorf("plist does not start with %q:\n%s", want, got)
	}
	if !strings.HasSuffix(got, "</dict>\n</plist>\n") {
		t.Errorf("plist does not end with the closing dict and plist:\n%s", got)
	}
}

func TestRenderPlistEscapingRoundTrips(t *testing.T) {
	cfg := escapingPlistConfig()
	logs := launchdLogs{stdout: `/l/&<>"'.out`, stderr: `/l/&<>"'.err`}
	_, top, err := decodePlist(renderPlist(cfg, logs))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := append([]any{cfg.Binary}, stringsToAny(cfg.Args)...)
	wantEnv := map[string]any{}
	for k, v := range cfg.Env {
		wantEnv[k] = v
	}
	checks := []struct {
		key  string
		want any
	}{
		{"Label", cfg.Name},
		{"WorkingDirectory", cfg.WorkingDir},
		{"UserName", cfg.User},
		{"StandardOutPath", logs.stdout},
		{"StandardErrorPath", logs.stderr},
		{"RunAtLoad", true},
		{"KeepAlive", true},
		{"ThrottleInterval", "5"},
	}
	for _, c := range checks {
		if got := top[c.key]; got != c.want {
			t.Errorf("%s = %#v, want %#v", c.key, got, c.want)
		}
	}
	if got, ok := top["ProgramArguments"].([]any); !ok || !slices.Equal(got, wantArgs) {
		t.Errorf("ProgramArguments = %#v, want %#v", top["ProgramArguments"], wantArgs)
	}
	got, ok := top["EnvironmentVariables"].(map[string]any)
	if !ok || len(got) != len(wantEnv) {
		t.Fatalf("EnvironmentVariables = %#v, want %#v", top["EnvironmentVariables"], wantEnv)
	}
	for k, v := range wantEnv {
		if got[k] != v {
			t.Errorf("EnvironmentVariables[%q] = %#v, want %#v", k, got[k], v)
		}
	}
}

func TestRenderPlistUserNameOnlyOnADaemonWithAUser(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		user  string
		want  bool
	}{
		{name: "daemon-with-user", scope: ScopeSystem, user: "demo", want: true},
		{name: "daemon-without-user", scope: ScopeSystem},
		{name: "agent-with-user", scope: ScopeUser, user: "demo"},
		{name: "agent-without-user", scope: ScopeUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Name: "demo", Binary: "/bin/demo", User: tt.user, Scope: tt.scope}
			keys, _, err := decodePlist(renderPlist(cfg, defaultLaunchdLogs(tt.scope, "demo", "/Users/me")))
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(keys, "UserName"); got != tt.want {
				t.Errorf("UserName present = %v, want %v (keys %q)", got, tt.want, keys)
			}
		})
	}
}

func TestDefaultLaunchdLogs(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		svc   string
		home  string
		want  string
	}{
		{name: "agent", scope: ScopeUser, svc: "demo", home: "/Users/me", want: "/Users/me/Library/Logs/demo/demo.log"},
		{name: "agent-home-trailing-slash", scope: ScopeUser, svc: "demo", home: "/Users/me/", want: "/Users/me/Library/Logs/demo/demo.log"},
		{name: "agent-dotted-name", scope: ScopeUser, svc: "org.example.demo", home: "/Users/me",
			want: "/Users/me/Library/Logs/org.example.demo/org.example.demo.log"},
		{name: "daemon", scope: ScopeSystem, svc: "demo", home: "", want: "/Library/Logs/demo/demo.log"},
		{name: "daemon-ignores-home", scope: ScopeSystem, svc: "demo", home: "/Users/me", want: "/Library/Logs/demo/demo.log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultLaunchdLogs(tt.scope, tt.svc, tt.home)
			if want := (launchdLogs{stdout: tt.want, stderr: tt.want}); got != want {
				t.Errorf("defaultLaunchdLogs(%v, %q, %q) = %+v, want %+v", tt.scope, tt.svc, tt.home, got, want)
			}
		})
	}
}

func TestLaunchdLogsWithDefaults(t *testing.T) {
	def := launchdLogs{stdout: "/d/out", stderr: "/d/err"}
	tests := []struct {
		name string
		in   launchdLogs
		want launchdLogs
	}{
		{name: "none", in: launchdLogs{}, want: def},
		{name: "stdout", in: launchdLogs{stdout: "/o"}, want: launchdLogs{stdout: "/o", stderr: "/d/err"}},
		{name: "stderr", in: launchdLogs{stderr: "/e"}, want: launchdLogs{stdout: "/d/out", stderr: "/e"}},
		{name: "both", in: launchdLogs{stdout: "/o", stderr: "/e"}, want: launchdLogs{stdout: "/o", stderr: "/e"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.withDefaults(def); got != tt.want {
				t.Errorf("%+v.withDefaults(%+v) = %+v, want %+v", tt.in, def, got, tt.want)
			}
		})
	}
}

func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// plistDecoder walks a property list token by token. Values decode to a
// string (<string> and <integer> text), a bool, an []any or a map[string]any.
type plistDecoder struct {
	d *xml.Decoder
}

// decodePlist checks that data is a <plist version="1.0"> holding exactly one
// <dict>, in which and in every nested <dict> each <key> is unique and
// followed by one value, and returns the top-level keys in order with the
// decoded dict.
func decodePlist(data []byte) ([]string, map[string]any, error) {
	p := plistDecoder{d: xml.NewDecoder(bytes.NewReader(data))}
	start, err := p.start()
	if err != nil {
		return nil, nil, err
	}
	if start.Name.Local != "plist" {
		return nil, nil, fmt.Errorf("root element is <%s>, want <plist>", start.Name.Local)
	}
	if len(start.Attr) != 1 || start.Attr[0].Name.Local != "version" || start.Attr[0].Value != "1.0" {
		return nil, nil, fmt.Errorf("root attributes are %v, want version=\"1.0\" alone", start.Attr)
	}
	start, err = p.start()
	if err != nil {
		return nil, nil, err
	}
	if start.Name.Local != "dict" {
		return nil, nil, fmt.Errorf("<plist> holds <%s>, want <dict>", start.Name.Local)
	}
	keys, top, err := p.dict()
	if err != nil {
		return nil, nil, err
	}
	tok, err := p.next()
	if err != nil {
		return nil, nil, err
	}
	if end, ok := tok.(xml.EndElement); !ok || end.Name.Local != "plist" {
		return nil, nil, fmt.Errorf("<plist> holds %#v after its <dict>, want </plist>", tok)
	}
	if tok, err := p.next(); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%#v after </plist>: %w", tok, err)
	}
	return keys, top, nil
}

// next returns the next token that is not whitespace, the XML declaration,
// the DOCTYPE or a comment. Any other text is an error.
func (p plistDecoder) next() (xml.Token, error) {
	for {
		tok, err := p.d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.ProcInst, xml.Directive, xml.Comment:
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return nil, fmt.Errorf("stray text %q", t)
			}
		default:
			return xml.CopyToken(tok), nil
		}
	}
}

// start returns the next token, which must be a start element.
func (p plistDecoder) start() (xml.StartElement, error) {
	tok, err := p.next()
	if err != nil {
		return xml.StartElement{}, err
	}
	s, ok := tok.(xml.StartElement)
	if !ok {
		return xml.StartElement{}, fmt.Errorf("got %#v, want a start element", tok)
	}
	if len(s.Attr) > 0 && s.Name.Local != "plist" {
		return xml.StartElement{}, fmt.Errorf("<%s> carries attributes %v", s.Name.Local, s.Attr)
	}
	return s, nil
}

// text returns the text of a leaf element up to its end tag.
func (p plistDecoder) text(name string) (string, error) {
	var b strings.Builder
	for {
		tok, err := p.d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			if t.Name.Local != name {
				return "", fmt.Errorf("</%s> closes <%s>", t.Name.Local, name)
			}
			return b.String(), nil
		default:
			return "", fmt.Errorf("<%s> holds %#v, want text only", name, tok)
		}
	}
}

// dict reads the entries of a <dict> whose start tag is consumed, up to and
// including its end tag.
func (p plistDecoder) dict() ([]string, map[string]any, error) {
	var keys []string
	m := map[string]any{}
	for {
		tok, err := p.next()
		if err != nil {
			return nil, nil, err
		}
		if end, ok := tok.(xml.EndElement); ok {
			if end.Name.Local != "dict" {
				return nil, nil, fmt.Errorf("</%s> closes <dict>", end.Name.Local)
			}
			return keys, m, nil
		}
		s, ok := tok.(xml.StartElement)
		if !ok || s.Name.Local != "key" {
			return nil, nil, fmt.Errorf("<dict> holds %#v where a <key> belongs", tok)
		}
		key, err := p.text("key")
		if err != nil {
			return nil, nil, err
		}
		if _, dup := m[key]; dup {
			return nil, nil, fmt.Errorf("key %q repeats within one <dict>", key)
		}
		s, err = p.start()
		if err != nil {
			return nil, nil, fmt.Errorf("value of key %q: %w", key, err)
		}
		v, err := p.value(s.Name.Local)
		if err != nil {
			return nil, nil, fmt.Errorf("value of key %q: %w", key, err)
		}
		keys = append(keys, key)
		m[key] = v
	}
}

// value reads the value whose start tag <name> is consumed.
func (p plistDecoder) value(name string) (any, error) {
	switch name {
	case "string":
		return p.text(name)
	case "integer":
		s, err := p.text(name)
		if err != nil {
			return nil, err
		}
		if _, err := strconv.Atoi(s); err != nil {
			return nil, fmt.Errorf("<integer> holds %q: %w", s, err)
		}
		return s, nil
	case "true", "false":
		s, err := p.text(name)
		if err != nil {
			return nil, err
		}
		if s != "" {
			return nil, fmt.Errorf("<%s/> holds %q, want nothing", name, s)
		}
		return name == "true", nil
	case "array":
		var out []any
		for {
			tok, err := p.next()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				if t.Name.Local != "array" {
					return nil, fmt.Errorf("</%s> closes <array>", t.Name.Local)
				}
				return out, nil
			case xml.StartElement:
				v, err := p.value(t.Name.Local)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			default:
				return nil, fmt.Errorf("<array> holds %#v", tok)
			}
		}
	case "dict":
		_, m, err := p.dict()
		return m, err
	default:
		return nil, fmt.Errorf("<%s> is not a plist value", name)
	}
}
