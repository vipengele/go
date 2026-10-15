// Package terminal detects whether the standard streams of a process are attached to a terminal.
//
// [Detect] returns the func a [spec.Env] carries as its Terminal field. It is the only package of
// the CLI framework that imports golang.org/x/term.
package terminal

import (
	"io"
	"os"
	"sync"

	"golang.org/x/term"

	"go.vipengele.org/cli/spec"
)

// Probes are the seams [DetectWith] reads the system through. The zero value uses the real
// system.
type Probes struct {
	// IsTerminal reports whether the file descriptor fd is a terminal. Nil means term.IsTerminal
	// from golang.org/x/term. It is called only for a stream that is an *os.File.
	IsTerminal func(fd uintptr) bool
}

// Detect returns a func reporting whether each of stdin, stdout and stderr is attached to a
// terminal, checked against the real system. It is DetectWith with the zero [Probes].
func Detect(stdin io.Reader, stdout, stderr io.Writer) func(spec.Stream) bool {
	return DetectWith(Probes{}, stdin, stdout, stderr)
}

// DetectWith returns a func reporting whether each of stdin, stdout and stderr is attached to a
// terminal, as p.IsTerminal answers for the stream's file descriptor.
//
// Each stream is checked lazily, on its first query, and at most once: every later query, from
// any goroutine, returns that first answer. A stream that is not a non-nil *os.File is not a
// terminal, and p.IsTerminal is not called for it. A [spec.Stream] other than [spec.Stdin],
// [spec.Stdout] and [spec.Stderr] is not a terminal. The returned func is safe for concurrent use.
func DetectWith(p Probes, stdin io.Reader, stdout, stderr io.Writer) func(spec.Stream) bool {
	isTerminal := p.IsTerminal
	if isTerminal == nil {
		isTerminal = realIsTerminal
	}
	in := once(isTerminal, stdin)
	out := once(isTerminal, stdout)
	errs := once(isTerminal, stderr)
	return func(s spec.Stream) bool {
		switch s {
		case spec.Stdin:
			return in()
		case spec.Stdout:
			return out()
		case spec.Stderr:
			return errs()
		default:
			return false
		}
	}
}

// once returns a func that checks stream with isTerminal on its first call and returns that
// answer on every call.
func once(isTerminal func(uintptr) bool, stream any) func() bool {
	return sync.OnceValue(func() bool {
		f, ok := stream.(*os.File)
		if !ok || f == nil {
			return false
		}
		return isTerminal(f.Fd())
	})
}

// realIsTerminal is the default [Probes.IsTerminal].
func realIsTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}
