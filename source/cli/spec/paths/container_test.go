package paths

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
)

// probes builds Probes on goos from the existing paths, the environment and the cgroup file. A
// nil cgroup is a missing file.
func probes(goos string, existing []string, env map[string]string, cgroup *string) Probes {
	return Probes{
		GOOS: goos,
		LookupEnv: func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
		Exists: func(path string) bool {
			for _, e := range existing {
				if e == path {
					return true
				}
			}
			return false
		},
		ReadFile: func(string) ([]byte, error) {
			if cgroup == nil {
				return nil, fs.ErrNotExist
			}
			return []byte(*cgroup), nil
		},
	}
}

func str(s string) *string { return &s }

func TestInContainerEachProbeAlone(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		env      map[string]string
		cgroup   *string
	}{
		{name: "dockerenv", existing: []string{"/.dockerenv"}},
		{name: "containerenv", existing: []string{"/run/.containerenv"}},
		{name: "container variable", env: map[string]string{"container": "podman"}},
		{name: "kubernetes variable", env: map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}},
		{name: "cgroup docker", cgroup: str("12:cpu:/docker/abc\n")},
		{name: "cgroup kubepods", cgroup: str("1:name=systemd:/kubepods/burstable/pod1\n")},
		{name: "cgroup containerd", cgroup: str("0::/system.slice/containerd.service\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !InContainer(probes("linux", tt.existing, tt.env, tt.cgroup)) {
				t.Error("InContainer = false, want true")
			}
		})
	}
}

func TestInContainerNoSignal(t *testing.T) {
	tests := []struct {
		name string
		p    Probes
	}{
		{name: "no probe", p: probes("linux", nil, nil, nil)},
		{name: "cgroup v2", p: probes("linux", nil, nil, str("0::/\n"))},
		{name: "empty variables count as unset", p: probes("linux", nil,
			map[string]string{"container": "", "KUBERNETES_SERVICE_HOST": ""}, nil)},
		{name: "cgroup read error", p: Probes{
			GOOS:      "linux",
			LookupEnv: func(string) (string, bool) { return "", false },
			Exists:    func(string) bool { return false },
			ReadFile:  func(string) ([]byte, error) { return nil, errors.New("denied") },
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if InContainer(tt.p) {
				t.Error("InContainer = true, want false")
			}
		})
	}
}

func TestInContainerOffLinuxIsFalse(t *testing.T) {
	env := map[string]string{"container": "docker", "KUBERNETES_SERVICE_HOST": "10.0.0.1"}
	for _, goos := range []string{"darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			p := probes(goos, []string{"/.dockerenv", "/run/.containerenv"}, env, str("1:cpu:/docker/abc\n"))
			if InContainer(p) {
				t.Error("InContainer = true, want false")
			}
		})
	}
}

func TestInContainerProbesOnlyTheDocumentedPaths(t *testing.T) {
	var existsPaths, readPaths []string
	p := Probes{
		GOOS:      "linux",
		LookupEnv: func(string) (string, bool) { return "", false },
		Exists: func(path string) bool {
			existsPaths = append(existsPaths, path)
			return false
		},
		ReadFile: func(path string) ([]byte, error) {
			readPaths = append(readPaths, path)
			return nil, fs.ErrNotExist
		},
	}
	if InContainer(p) {
		t.Fatal("InContainer = true, want false")
	}
	if want := []string{"/.dockerenv", "/run/.containerenv"}; !reflect.DeepEqual(existsPaths, want) {
		t.Errorf("existence checks = %v, want %v", existsPaths, want)
	}
	if want := []string{"/proc/1/cgroup"}; !reflect.DeepEqual(readPaths, want) {
		t.Errorf("reads = %v, want %v", readPaths, want)
	}
}

func TestInContainerZeroProbes(t *testing.T) {
	// The answer depends on where the test runs; the zero value must only work.
	_ = InContainer(Probes{})
}
