package spec

import (
	"bytes"
	"io"
)

// Env is the environment one execution of a command runs in: its standard streams, its working
// directory and its environment variables. A run function reaches the process only through it,
// so a test runs a command against buffers and a fixed environment.
type Env struct {
	// Stdin is the standard input. Nil reads as empty.
	Stdin io.Reader
	// Stdout is the standard output. Nil discards what is written.
	Stdout io.Writer
	// Stderr is the standard error. Nil discards what is written.
	Stderr io.Writer
	// WorkDir is the working directory. "" means the directory of the process, against which a
	// relative path resolves.
	WorkDir string
	// LookupEnv returns an environment variable's value and whether it is set, as os.LookupEnv
	// does. Nil is an empty environment.
	LookupEnv func(key string) (string, bool)
	// Terminal reports whether a standard stream is attached to a terminal; IsTerminal consults
	// it. It may be called concurrently, and IsTerminal calls it on every query, so an injected
	// func answers each call afresh. Caching the answer is a property of terminal.Detect, the
	// implementation cli/core wires, not of Env. Nil leaves IsTerminal to answer false for a
	// stream that is not a file and to panic for one that is.
	Terminal func(Stream) bool
}

// Resolved returns a copy of e in which Stdin, Stdout, Stderr and LookupEnv, when nil, hold an
// inert default: an empty Stdin, Stdout and Stderr that discard, and a LookupEnv that finds
// nothing. A nil e resolves to an Env made only of those defaults. WorkDir and Terminal are copied
// unchanged: a nil Terminal stays nil, since no default could know whether a file is a terminal.
// e itself is not modified.
func (e *Env) Resolved() *Env {
	var r Env
	if e != nil {
		r = *e
	}
	if r.Stdin == nil {
		r.Stdin = bytes.NewReader(nil)
	}
	if r.Stdout == nil {
		r.Stdout = io.Discard
	}
	if r.Stderr == nil {
		r.Stderr = io.Discard
	}
	if r.LookupEnv == nil {
		r.LookupEnv = lookupNothing
	}
	return &r
}

func lookupNothing(string) (string, bool) {
	return "", false
}
