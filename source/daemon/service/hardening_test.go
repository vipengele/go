package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultHardening(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		want  Hardening
	}{
		{"system", ScopeSystem, Hardening{
			NoNewPrivileges: true, PrivateTmp: true, ProtectSystem: "full", ProtectHome: "read-only",
			ProtectKernelTunables: true, ProtectControlGroups: true, RestrictSUIDSGID: true,
		}},
		{"user", ScopeUser, Hardening{NoNewPrivileges: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultHardening(tc.scope)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DefaultHardening = %+v, want %+v", got, tc.want)
			}
			if err := got.validate(); err != nil {
				t.Fatalf("default does not validate: %v", err)
			}
		})
	}
}

func TestHardeningValidate(t *testing.T) {
	tests := []struct {
		name string
		h    Hardening
		want []string
	}{
		{"zero", Hardening{}, nil},
		{"valid", Hardening{ProtectSystem: "strict", ProtectHome: "tmpfs", ReadWritePaths: []string{"/var/lib/x", "/"}}, nil},
		{"ProtectSystem true", Hardening{ProtectSystem: "true"}, nil},
		{"ProtectHome true", Hardening{ProtectHome: "true"}, nil},
		{"bad ProtectSystem", Hardening{ProtectSystem: "yes"}, []string{"ProtectSystem"}},
		{"ProtectSystem case", Hardening{ProtectSystem: "Full"}, []string{"ProtectSystem"}},
		{"bad ProtectHome", Hardening{ProtectHome: "full"}, []string{"ProtectHome"}},
		{"relative path", Hardening{ReadWritePaths: []string{"var/lib"}}, []string{"ReadWritePaths[0]"}},
		{"empty path", Hardening{ReadWritePaths: []string{""}}, []string{"ReadWritePaths[0]"}},
		{"windows path", Hardening{ReadWritePaths: []string{`C:\x`}}, []string{"ReadWritePaths[0]"}},
		{"newline path", Hardening{ReadWritePaths: []string{"/a\nb"}}, []string{"ReadWritePaths[0]"}},
		{"nul path", Hardening{ReadWritePaths: []string{"/a\x00b"}}, []string{"ReadWritePaths[0]"}},
		{"del path", Hardening{ReadWritePaths: []string{"/a\x7fb"}}, []string{"ReadWritePaths[0]"}},
		{"space in path", Hardening{ReadWritePaths: []string{"/a b"}}, nil},
		{"C1 control U+0080 in path", Hardening{ReadWritePaths: []string{"/a\u0080b"}}, []string{"ReadWritePaths[0]"}},
		{"C1 control U+009F in path", Hardening{ReadWritePaths: []string{"/a\u009fb"}}, []string{"ReadWritePaths[0]"}},
		{"no-break space U+00A0 in path", Hardening{ReadWritePaths: []string{"/a\u00a0b"}}, nil},
		{"join order", Hardening{
			ProtectSystem: "x", ProtectHome: "y", ReadWritePaths: []string{"/ok", "rel", "/t\tab"},
		}, []string{"ProtectSystem", "ProtectHome", "ReadWritePaths[1]", "ReadWritePaths[2]"}},
	}
	for _, goos := range []string{"linux", "plan9"} {
		for _, tc := range tests {
			t.Run(goos+"/"+tc.name, func(t *testing.T) {
				_, err := New(validConfig(goos), WithGOOS(goos), WithSystemdHardening(tc.h), notInContainer())
				if tc.want == nil {
					if errors.Is(err, ErrInvalidConfig) {
						t.Fatalf("New = %v, want no ErrInvalidConfig", err)
					}
					return
				}
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("New = %v, want ErrInvalidConfig", err)
				}
				errs := violations(t, err)
				if len(errs) != len(tc.want) {
					t.Fatalf("got %d violations %v, want %d", len(errs), err, len(tc.want))
				}
				for i, e := range errs {
					if !strings.Contains(e.Error(), "Hardening."+tc.want[i]+":") {
						t.Errorf("violation %d = %q, want it to name %s", i, e, tc.want[i])
					}
				}
			})
		}
	}
}

func TestWithSystemdHardeningCopiesPaths(t *testing.T) {
	paths := []string{"/a"}
	var o options
	WithSystemdHardening(Hardening{ReadWritePaths: paths})(&o)
	paths[0] = "rel"
	if o.hardening == nil || o.hardening.ReadWritePaths[0] != "/a" {
		t.Fatalf("option kept the caller's slice: %+v", o.hardening)
	}
}
