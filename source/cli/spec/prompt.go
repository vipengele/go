package spec

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Confirm and Prompt ask a question on the standard error and read the answer, one line, from the
// standard input. They ask only when the standard input is a terminal, as [Env.IsTerminal]
// reports for [Stdin]; neither quiet nor JSON output changes that, and quiet does not drop the
// question, which the run cannot proceed without. Nothing is written to the standard output.
//
// When the standard input is not a terminal they read nothing: a question with a default and not
// Required answers with its default and writes nothing, and any other question returns a bare
// [*UsageError] naming the question and the options' Flag, which go.vipengele.org/cli/core exits 2
// for and prints as one line.
//
// The answer is read a byte at a time up to and including the first newline, so whatever follows
// it is left unread in the standard input. Leading and trailing white space, a carriage return
// included, is trimmed from it. An end of input before any byte returns [ErrAborted] and writes
// nothing, whatever the default; an end of input after some bytes takes those bytes as the answer.
// An answer is never asked for twice: one that is not accepted returns an error that says what is.
//
// The Invocation does not carry the run's context, so an interrupt is not observed while a
// question waits for its answer: the first interrupt cancels the context and the read goes on
// until Enter, and the second abandons the run with exit code 130. A run function that asks
// checks its context after the answer. An Invocation whose Env is nil, such as &Invocation{}, has
// no terminal and never asks.

// ErrAborted is the error Confirm and Prompt return when the standard input ends before an answer
// is typed, as when a person presses Ctrl-D at the question. It exits 1, and neither Confirm nor
// Prompt writes anything for it.
var ErrAborted = errors.New("aborted")

// ConfirmDefault is the answer a [ConfirmOptions] gives when no answer is typed. The zero value is
// [NoDefault], and any value other than the constants below is read as NoDefault.
type ConfirmDefault int

const (
	// NoDefault gives no answer: Enter alone is not accepted, and the hint is "[y/n]".
	NoDefault ConfirmDefault = iota
	// DefaultYes answers yes, and the hint is "[Y/n]".
	DefaultYes
	// DefaultNo answers no, and the hint is "[y/N]".
	DefaultNo
)

// ConfirmOptions configures [Invocation.Confirm]. The zero value asks with no default and names
// no flag.
type ConfirmOptions struct {
	// Default is the answer Enter alone gives at a terminal, and, unless Required, the answer when
	// the standard input is not a terminal.
	Default ConfirmDefault
	// Required makes the question need a person: when the standard input is not a terminal the
	// default does not apply and Confirm returns a [*UsageError].
	Required bool
	// Flag is the flag that answers the question without asking, printed as given, such as
	// "--yes", in the [*UsageError]. Empty names no flag.
	Flag string
}

// PromptOptions configures [Invocation.Prompt]. The zero value asks with no default and names no
// flag.
type PromptOptions struct {
	// Default is the answer Enter alone gives at a terminal, and, unless Required, the answer when
	// the standard input is not a terminal. Empty is no default.
	Default string
	// Required makes the question need a person: when the standard input is not a terminal the
	// default does not apply and Prompt returns a [*UsageError].
	Required bool
	// Flag is the flag that gives the value without asking, printed as given, such as
	// "--name NAME", in the [*UsageError]. Empty names no flag.
	Flag string
}

// Confirm asks a yes-or-no question: it writes question, a space, the hint of opts.Default and a
// space to the standard error, and reads the answer. y, yes, n and no are accepted in any case;
// Enter alone gives the default. It returns true for yes.
func (inv *Invocation) Confirm(question string, opts ConfirmOptions) (bool, error) {
	dflt, hasDefault := opts.Default.answer()
	if !inv.Env.IsTerminal(Stdin) {
		if !hasDefault || opts.Required {
			return false, &UsageError{Question: question, Flag: opts.Flag}
		}
		return dflt, nil
	}
	answer, err := inv.ask(question + " " + opts.Default.hint() + " ")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	case "":
		if hasDefault {
			return dflt, nil
		}
	}
	return false, fmt.Errorf("%q is not an answer to %q: answer y, yes, n or no", answer, question)
}

// Prompt asks for a value: it writes question, a space, the default in brackets and a space when
// there is a default, and a space alone when there is none, to the standard error, and reads the
// answer. Enter alone gives the default, and with no default is not accepted.
func (inv *Invocation) Prompt(question string, opts PromptOptions) (string, error) {
	if !inv.Env.IsTerminal(Stdin) {
		if opts.Default == "" || opts.Required {
			return "", &UsageError{Question: question, Flag: opts.Flag}
		}
		return opts.Default, nil
	}
	text := question + " "
	if opts.Default != "" {
		text += "[" + opts.Default + "] "
	}
	answer, err := inv.ask(text)
	if err != nil {
		return "", err
	}
	if answer != "" {
		return answer, nil
	}
	if opts.Default != "" {
		return opts.Default, nil
	}
	return "", fmt.Errorf("no answer to %q: type a value and press Enter", question)
}

// answer returns the answer d gives and whether it gives one. A value other than [DefaultYes]
// and [DefaultNo] gives none.
func (d ConfirmDefault) answer() (yes, ok bool) {
	switch d {
	case DefaultYes:
		return true, true
	case DefaultNo:
		return false, true
	default:
		return false, false
	}
}

func (d ConfirmDefault) hint() string {
	switch d {
	case DefaultYes:
		return "[Y/n]"
	case DefaultNo:
		return "[y/N]"
	default:
		return "[y/n]"
	}
}

// ask writes text to the standard error and returns the line read from the standard input,
// trimmed.
func (inv *Invocation) ask(text string) (string, error) {
	env := inv.Env.Resolved()
	_, _ = io.WriteString(env.Stderr, text)
	line, err := readLine(env.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readLine reads r a byte at a time up to and including the first newline, which it drops, so
// nothing after the newline is consumed. An end of input before any byte is [ErrAborted]; after
// some bytes it ends the line.
func readLine(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		switch {
		case errors.Is(err, io.EOF):
			if len(line) == 0 {
				return "", ErrAborted
			}
			return string(line), nil
		case err != nil:
			return "", fmt.Errorf("read answer: %w", err)
		}
	}
}
