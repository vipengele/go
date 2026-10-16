package spec

import "fmt"

// The message helpers below write one line each to the standard error, styled by the Styler
// [Output.Styler] returns for [Stderr], with a newline appended. [Invocation.Data] writes a
// command's result to the standard output. Every helper takes a format and arguments as
// [fmt.Printf] does, so go vet checks each call site; a string that is not a constant format is
// passed as an argument, inv.Info("%s", msg), so a % in it is written as is.
//
// Quiet drops Info, Success, Warning and Detail; Error and Data always write. JSON changes none of
// them: messages go to the standard error, which carries no JSON. An Invocation whose Env is nil,
// such as &Invocation{}, is supported: its writes are discarded, as [Env.Resolved] defines. The
// message helpers ignore a failed write, since a message that cannot be shown has nowhere better
// to go; Data returns it.

// Info writes an informational message: [GlyphInfo], a space and the formatted text, in blue.
// Quiet drops it.
func (inv *Invocation) Info(format string, args ...any) {
	if inv.Output.Quiet() {
		return
	}
	inv.message(inv.stderrStyler().Info, format, args...)
}

// Success writes a message that an operation succeeded: [GlyphSuccess], a space and the
// formatted text, in green. Quiet drops it.
func (inv *Invocation) Success(format string, args ...any) {
	if inv.Output.Quiet() {
		return
	}
	inv.message(inv.stderrStyler().Success, format, args...)
}

// Warning writes a warning: [GlyphWarning], a space and the formatted text, in yellow. Quiet
// drops it.
func (inv *Invocation) Warning(format string, args ...any) {
	if inv.Output.Quiet() {
		return
	}
	inv.message(inv.stderrStyler().Warning, format, args...)
}

// Error writes an error message: [GlyphError], a space and the formatted text, in red. Quiet does
// not drop it.
//
// It is for an error the command shows itself: one that does not end the run, or one shown before
// the run function returns it wrapped in [Reported], so that it is not shown twice. An error the
// run function returns unwrapped is printed by the App's renderer instead, as "<tool>: msg"
// without glyph or colour; quiet does not drop that either.
func (inv *Invocation) Error(format string, args ...any) {
	inv.message(inv.stderrStyler().Error, format, args...)
}

// Detail writes a dimmed line of extra detail, with no glyph. Only a verbose run writes it, and
// quiet drops it.
func (inv *Invocation) Detail(format string, args ...any) {
	if !inv.Output.Verbose() || inv.Output.Quiet() {
		return
	}
	inv.message(inv.stderrStyler().Dim, format, args...)
}

// Data writes the formatted text to the standard output exactly: no styling, no newline appended.
// It is the command's result, so neither quiet nor any other setting drops it. It returns the
// error of the write.
func (inv *Invocation) Data(format string, args ...any) error {
	_, err := fmt.Fprintf(inv.Env.Resolved().Stdout, format, args...)
	return err
}

func (inv *Invocation) stderrStyler() Styler {
	return inv.Output.Styler(Stderr)
}

// message writes the formatted text, styled by style, and a newline to the standard error.
func (inv *Invocation) message(style func(string) string, format string, args ...any) {
	_, _ = fmt.Fprintln(inv.Env.Resolved().Stderr, style(fmt.Sprintf(format, args...)))
}
