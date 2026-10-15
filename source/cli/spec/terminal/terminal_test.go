package terminal_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/terminal"
)

// countingProbe answers true for the fds in terminals and counts its calls per fd.
type countingProbe struct {
	mu        sync.Mutex
	terminals map[uintptr]bool
	calls     map[uintptr]int
}

func newCountingProbe(terminals ...uintptr) *countingProbe {
	p := &countingProbe{terminals: map[uintptr]bool{}, calls: map[uintptr]int{}}
	for _, fd := range terminals {
		p.terminals[fd] = true
	}
	return p
}

func (p *countingProbe) isTerminal(fd uintptr) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[fd]++
	return p.terminals[fd]
}

func (p *countingProbe) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		n += c
	}
	return n
}

func (p *countingProbe) callsFor(fd uintptr) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[fd]
}

// tempFiles creates n distinct regular files, closed when the test ends.
func tempFiles(t *testing.T, n int) []*os.File {
	t.Helper()
	files := make([]*os.File, n)
	for i := range files {
		f, err := os.CreateTemp(t.TempDir(), "stream")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		files[i] = f
	}
	return files
}

func TestDetectWithAnswersEachStreamSeparately(t *testing.T) {
	t.Parallel()
	f := tempFiles(t, 3)
	for _, tc := range []struct {
		name     string
		terminal *os.File
		want     spec.Stream
	}{
		{"stdin", f[0], spec.Stdin},
		{"stdout", f[1], spec.Stdout},
		{"stderr", f[2], spec.Stderr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := newCountingProbe(tc.terminal.Fd())
			detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal}, f[0], f[1], f[2])
			for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
				if got := detect(s); got != (s == tc.want) {
					t.Errorf("detect(%v) = %v, want %v", s, got, s == tc.want)
				}
			}
		})
	}
}

func TestDetectWithProbesEachStreamAtMostOnce(t *testing.T) {
	t.Parallel()
	f := tempFiles(t, 3)
	probe := newCountingProbe(f[1].Fd())
	detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal}, f[0], f[1], f[2])

	if probe.total() != 0 {
		t.Fatalf("probe called %d times before any query, want 0", probe.total())
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 8 {
				for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
					if got, want := detect(s), s == spec.Stdout; got != want {
						t.Errorf("detect(%v) = %v, want %v", s, got, want)
					}
				}
			}
		})
	}
	wg.Wait()
	for i, file := range f {
		if n := probe.callsFor(file.Fd()); n != 1 {
			t.Errorf("probe called %d times for stream %d, want 1", n, i)
		}
	}
}

func TestDetectWithChecksOnlyTheQueriedStream(t *testing.T) {
	t.Parallel()
	f := tempFiles(t, 3)
	probe := newCountingProbe()
	detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal}, f[0], f[1], f[2])

	detect(spec.Stderr)
	if probe.total() != 1 || probe.callsFor(f[2].Fd()) != 1 {
		t.Errorf("probe calls = %v, want one call for stderr only", probe.calls)
	}
}

func TestDetectWithNonFileStreamIsNotATerminal(t *testing.T) {
	t.Parallel()
	var nilFile *os.File
	probe := newCountingProbe()
	detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal},
		bytes.NewReader(nil), &bytes.Buffer{}, nilFile)
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if detect(s) {
			t.Errorf("detect(%v) = true, want false", s)
		}
	}
	if n := probe.total(); n != 0 {
		t.Errorf("probe called %d times, want 0", n)
	}
}

func TestDetectWithNilStreamsAreNotTerminals(t *testing.T) {
	t.Parallel()
	probe := newCountingProbe()
	detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal}, nil, nil, nil)
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if detect(s) {
			t.Errorf("detect(%v) = true, want false", s)
		}
	}
	if n := probe.total(); n != 0 {
		t.Errorf("probe called %d times, want 0", n)
	}
}

func TestDetectWithUnknownStreamIsNotATerminal(t *testing.T) {
	t.Parallel()
	f := tempFiles(t, 3)
	probe := newCountingProbe(f[0].Fd(), f[1].Fd(), f[2].Fd())
	detect := terminal.DetectWith(terminal.Probes{IsTerminal: probe.isTerminal}, f[0], f[1], f[2])
	for _, s := range []spec.Stream{-1, 3, 42} {
		if detect(s) {
			t.Errorf("detect(%v) = true, want false", s)
		}
	}
	if n := probe.total(); n != 0 {
		t.Errorf("probe called %d times, want 0", n)
	}
}

func TestDetectRegularFileIsNotATerminal(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	detect := terminal.Detect(f, f, f)
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if detect(s) {
			t.Errorf("detect(%v) on regular file %s = true, want false", s, filepath.Base(f.Name()))
		}
	}
}

func TestDetectServesAsEnvTerminal(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	env := &spec.Env{Stdin: f, Stdout: f, Stderr: f, Terminal: terminal.Detect(f, f, f)}
	for _, s := range []spec.Stream{spec.Stdin, spec.Stdout, spec.Stderr} {
		if env.IsTerminal(s) {
			t.Errorf("env.IsTerminal(%v) = true, want false", s)
		}
	}
}
