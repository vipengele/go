package selfupdate

import (
	"hash/fnv"
	"testing"
	"time"
)

func TestJitterIsBelowATenthOfTheInterval(t *testing.T) {
	t.Parallel()

	paths := []string{"", "/usr/local/bin/tool", `C:\Program Files\tool\tool.exe`, "/opt/a", "/opt/b"}
	intervals := []time.Duration{time.Hour, DefaultInterval, 7 * 24 * time.Hour, 10, 11, 1}
	for _, interval := range intervals {
		for _, path := range paths {
			got := jitter(path, interval)
			if got < 0 || (interval >= 10 && got >= interval/10) {
				t.Errorf("jitter(%q, %v) = %v, want in [0, %v)", path, interval, got, interval/10)
			}
			if interval < 10 && got != 0 {
				t.Errorf("jitter(%q, %v) = %v, want 0 for an interval with no window", path, interval, got)
			}
		}
	}
}

func TestJitterIsStableForAPath(t *testing.T) {
	t.Parallel()

	const path = "/usr/local/bin/tool"
	first := jitter(path, DefaultInterval)
	for range 10 {
		if got := jitter(path, DefaultInterval); got != first {
			t.Fatalf("jitter(%q) = %v, then %v, want the same", path, first, got)
		}
	}
}

func TestJitterIsTheFNV1aHashOfThePathModuloTheWindow(t *testing.T) {
	t.Parallel()

	const path = "/usr/local/bin/tool"
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	want := time.Duration(h.Sum64() % uint64(DefaultInterval/10))

	if got := jitter(path, DefaultInterval); got != want {
		t.Errorf("jitter(%q, %v) = %v, want %v", path, DefaultInterval, got, want)
	}
}

func TestJitterDiffersBetweenPaths(t *testing.T) {
	t.Parallel()

	a, b := jitter("/opt/a/tool", DefaultInterval), jitter("/opt/b/tool", DefaultInterval)
	if a == b {
		t.Errorf("jitter of two paths = %v for both, want them spread", a)
	}
}
