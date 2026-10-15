package spec

import (
	"reflect"
	"strconv"
)

// Stream names one of the standard streams of an Env.
type Stream int

const (
	// Stdin is the standard input, Env.Stdin.
	Stdin Stream = iota
	// Stdout is the standard output, Env.Stdout.
	Stdout
	// Stderr is the standard error, Env.Stderr.
	Stderr
)

// String returns "stdin", "stdout" or "stderr", and "Stream(n)" for any other value.
func (s Stream) String() string {
	switch s {
	case Stdin:
		return "stdin"
	case Stdout:
		return "stdout"
	case Stderr:
		return "stderr"
	default:
		return "Stream(" + strconv.Itoa(int(s)) + ")"
	}
}

// IsTerminal reports whether stream s of e is attached to a terminal.
//
// A non-nil Terminal answers, and is called on every query. With a nil Terminal, a stream that
// is not file-like — a buffer, a nil or typed-nil stream, any value without an Fd() uintptr
// method — is not a terminal, and a file-like stream panics: only a real terminal check can answer for a file,
// and false would be a guess. A nil e has no streams and no Terminal, and answers false. A Stream
// other than Stdin, Stdout and Stderr is never a terminal, and Terminal is not called for it.
func (e *Env) IsTerminal(s Stream) bool {
	if e == nil {
		return false
	}
	var stream any
	switch s {
	case Stdin:
		stream = e.Stdin
	case Stdout:
		stream = e.Stdout
	case Stderr:
		stream = e.Stderr
	default:
		return false
	}
	if e.Terminal != nil {
		return e.Terminal(s)
	}
	if _, ok := stream.(interface{ Fd() uintptr }); ok && !isNilPointer(stream) {
		panic("spec: Env.IsTerminal(" + s.String() + "): the stream is a file and Env.Terminal is nil; " +
			"build the Env with cli/core or set Env.Terminal")
	}
	return false
}

// isNilPointer reports whether v holds a nil pointer: a typed-nil stream such as a nil *os.File
// has an Fd method yet no file behind it, so it is no more a terminal than a nil stream.
func isNilPointer(v any) bool {
	r := reflect.ValueOf(v)
	return r.Kind() == reflect.Pointer && r.IsNil()
}
