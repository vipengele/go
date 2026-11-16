package wire_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/daemon/service"
	"go.vipengele.org/daemon/servicetest/internal/wire"
)

func config() service.Config {
	return service.Config{
		Name:        "demo",
		Description: "a demo",
		Binary:      "/usr/local/bin/demo",
		Args:        []string{"run", "--flag"},
		Env:         map[string]string{"A": "1", "B": "2"},
		WorkingDir:  "/var/lib/demo",
		User:        "demo",
		Scope:       service.ScopeSystem,
	}
}

func fullRequest() wire.Request {
	return wire.Request{
		Op:     wire.OpInstall,
		Config: config(),
		Notify: &wire.Notify{Watchdog: 30 * time.Second},
		Hardening: &service.Hardening{
			NoNewPrivileges:       true,
			PrivateTmp:            true,
			ProtectSystem:         "strict",
			ProtectHome:           "tmpfs",
			ProtectKernelTunables: true,
			ProtectControlGroups:  true,
			RestrictSUIDSGID:      true,
			ReadWritePaths:        []string{"/var/lib/demo"},
		},
		Identity: &spec.Identity{
			Name: "demo", Owner: "acme", Repo: "demo", EnvPrefix: "DEMO", DirName: "demo",
		},
	}
}

func TestRequestRoundTripsThroughJSON(t *testing.T) {
	for _, op := range []wire.Op{wire.OpInstall, wire.OpUninstall, wire.OpStatus} {
		in := fullRequest()
		in.Op = op
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out wire.Request
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Errorf("round trip of %s differs:\n in: %+v\nout: %+v", op, in, out)
		}
	}
}

func TestZeroRequestRoundTripsThroughJSON(t *testing.T) {
	var in wire.Request
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out wire.Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip differs:\n in: %+v\nout: %+v", in, out)
	}
}

func TestOptionsAlwaysDisableContainerDetection(t *testing.T) {
	r := wire.Request{Config: config()}
	opts := r.Options()
	if len(opts) != 1 {
		t.Fatalf("a request with no settings yields %d options, want 1", len(opts))
	}
	// The container variable makes detection report true; WithContainer must win.
	t.Setenv("DEMO_CONTAINER", "1")
	r.Identity = &spec.Identity{Name: "demo", Owner: "acme", Repo: "demo", EnvPrefix: "DEMO", DirName: "demo"}
	if _, err := service.New(r.Config, append(r.Options(), service.WithGOOS("linux"))...); err != nil {
		var ce *service.ContainerError
		if errors.As(err, &ce) {
			t.Fatalf("New refused as inside a container: %v", err)
		}
		t.Fatal(err)
	}
}

func TestOptionsCountReflectsSetSettings(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*wire.Request)
		want int
	}{
		{"none", func(*wire.Request) {}, 1},
		{"notify", func(r *wire.Request) { r.Notify = &wire.Notify{} }, 2},
		{"hardening", func(r *wire.Request) { r.Hardening = &service.Hardening{} }, 2},
		{"identity", func(r *wire.Request) { r.Identity = &spec.Identity{} }, 2},
		{"all", func(r *wire.Request) { *r = fullRequest() }, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := wire.Request{Config: config()}
			tt.mut(&r)
			if got := len(r.Options()); got != tt.want {
				t.Errorf("got %d options, want %d", got, tt.want)
			}
		})
	}
}

// Each setting is observable through the validation New applies to it, and
// only when the setting is present.
func TestOptionsApplyEachSetting(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*wire.Request)
		invalid bool
	}{
		{"valid full request", func(r *wire.Request) { *r = fullRequest() }, false},
		{"no settings", func(*wire.Request) {}, false},
		{"negative watchdog", func(r *wire.Request) { r.Notify = &wire.Notify{Watchdog: -time.Second} }, true},
		{"sub-millisecond watchdog", func(r *wire.Request) { r.Notify = &wire.Notify{Watchdog: time.Microsecond} }, true},
		{"zero watchdog", func(r *wire.Request) { r.Notify = &wire.Notify{} }, false},
		{"invalid hardening", func(r *wire.Request) { r.Hardening = &service.Hardening{ProtectSystem: "bogus"} }, true},
		{"zero hardening", func(r *wire.Request) { r.Hardening = &service.Hardening{} }, false},
		{"invalid identity", func(r *wire.Request) { r.Identity = &spec.Identity{} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := wire.Request{Config: config()}
			tt.mut(&r)
			_, err := service.New(r.Config, append(r.Options(), service.WithGOOS("linux"))...)
			if tt.invalid {
				if !errors.Is(err, service.ErrInvalidConfig) {
					t.Fatalf("New error = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
		})
	}
}
