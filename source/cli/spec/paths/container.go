package paths

import (
	"bytes"
	"os"
	"runtime"
	"strconv"

	"go.vipengele.org/cli/spec"
)

// The files and variables [InContainer] probes.
const (
	dockerEnvFile    = "/.dockerenv"
	podmanEnvFile    = "/run/.containerenv"
	initCgroupFile   = "/proc/1/cgroup"
	containerEnvVar  = "container"
	kubernetesEnvVar = "KUBERNETES_SERVICE_HOST"
	linuxGOOS        = "linux"
)

// cgroupMarkers are the words in /proc/1/cgroup that name a container runtime.
func cgroupMarkers() [][]byte {
	return [][]byte{[]byte("docker"), []byte("kubepods"), []byte("containerd")}
}

// Probes are the seams [InContainer] reads the system through. The zero value uses the real
// system.
type Probes struct {
	// GOOS is the operating system, as a value of runtime.GOOS. "" means runtime.GOOS. Anything
	// but "linux" is never a container.
	GOOS string
	// LookupEnv returns an environment variable's value and whether it is set, as os.LookupEnv
	// does. Nil means os.LookupEnv. A variable set to "" counts as unset.
	LookupEnv func(key string) (string, bool)
	// Exists reports whether a path exists. Nil means os.Stat succeeds on it.
	Exists func(path string) bool
	// ReadFile returns a file's contents, as os.ReadFile does. Nil means os.ReadFile. An error
	// is no signal.
	ReadFile func(path string) ([]byte, error)
}

// InContainer reports whether the process appears to run inside a container. It is a heuristic:
// it can miss a container and, rarely, name one that is not.
//
// On Linux it is true when /.dockerenv or /run/.containerenv exists, when the container or
// KUBERNETES_SERVICE_HOST environment variable is set to a non-empty value, or when
// /proc/1/cgroup mentions docker, kubepods or containerd. On any other operating system it is
// false. The cgroup signal is unreliable under cgroup v2, where a container sees "0::/", so a
// container that none of the other signals reveal goes undetected. A cgroup file that cannot be
// read is no signal.
//
// InContainer changes no path; the caller decides what to do with the answer.
func InContainer(p Probes) bool {
	goos := p.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != linuxGOOS {
		return false
	}
	lookup := p.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	exists := p.Exists
	if exists == nil {
		exists = statExists
	}
	read := p.ReadFile
	if read == nil {
		read = os.ReadFile
	}

	if exists(dockerEnvFile) || exists(podmanEnvFile) {
		return true
	}
	for _, name := range []string{containerEnvVar, kubernetesEnvVar} {
		if v, ok := lookup(name); ok && v != "" {
			return true
		}
	}
	data, err := read(initCgroupFile)
	if err != nil {
		return false
	}
	for _, m := range cgroupMarkers() {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// InContainerFor reports whether the process runs inside a container, letting the user of the
// tool override the [InContainer] heuristic with the <PREFIX>_CONTAINER environment variable,
// where PREFIX is id.EnvPrefix.
//
// An unset or empty variable defers to [InContainer]. A value strconv.ParseBool accepts decides:
// "1" and "true" force true, "0" and "false" force false. Any other non-empty value, such as
// "yes", forces true. The override applies on every operating system; only the heuristic is
// Linux-only. An id whose EnvPrefix is invalid has no variable, so the heuristic answers.
//
// InContainerFor changes no path; the caller decides what to do with the answer.
func InContainerFor(id spec.Identity, p Probes) bool {
	name, err := id.EnvVar("CONTAINER")
	if err != nil {
		return InContainer(p)
	}
	lookup := p.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	v, ok := lookup(name)
	if !ok || v == "" {
		return InContainer(p)
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

// statExists reports whether os.Stat succeeds on path, the default [Probes.Exists].
func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
