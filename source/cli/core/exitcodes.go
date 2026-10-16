package cli

import (
	"errors"
	"fmt"
)

// ExitInterrupted is the exit code of a run that the process was interrupted out of. It is the
// conventional 128 plus SIGINT, and no option can give it to an error. The entry points of this
// package return it when an interrupt cancelled the run's context and the run returns
// [context.Canceled], and on a second interrupt, without waiting for the run.
const ExitInterrupted = 130

// maxExitCode is the highest exit code an option can register: a shell reports 126 and above for
// its own failures, and a code above 255 does not survive the exit status.
const maxExitCode = 125

// registrableExitCode reports whether an option can give code to an error: 1, and 3 through 125.
// 0, [ExitUsage] and [ExitInterrupted] are reserved for the runs they describe: a run function
// asks for [ExitUsage] by returning a [spec.UsageError], never by a code.
func registrableExitCode(code int) bool {
	return code >= ExitError && code <= maxExitCode && code != ExitUsage
}

// exitMatcher maps an error to the exit code an option gives it. For an error the option does not
// match it returns 0 and false.
type exitMatcher func(err error) (int, bool)

// exitMatchers is the ordered list of matchers the options add, in option order.
// Nothing changes it after [New], so concurrent executions share it.
type exitMatchers []exitMatcher

// code returns the code of the first matcher that matches err; when none does it returns 0 and
// false. The
// order is the matchers' registration order, whatever the order of the errors a joined err holds.
func (m exitMatchers) code(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	for _, match := range m {
		if code, ok := match(err); ok {
			return code, true
		}
	}
	return 0, false
}

// sentinelMatcher returns the matcher of errors.Is(err, sentinel) for code. It panics when code is
// not registrable, so [New] refuses the option that names it.
func sentinelMatcher(code int, sentinel error) exitMatcher {
	if !registrableExitCode(code) {
		panic(fmt.Sprintf("cli: exit code %d cannot be registered: it must be 1 or 3 through %d "+
			"(0, %d and %d are reserved)", code, maxExitCode, ExitUsage, ExitInterrupted))
	}
	return func(err error) (int, bool) {
		if errors.Is(err, sentinel) {
			return code, true
		}
		return 0, false
	}
}

// typedMatcher returns the matcher of errors.As(err, &E) that takes its code from fn. A code fn
// returns that is not registrable is no match, because the code is known only when an error
// matches, long after [New].
func typedMatcher[E error](fn func(E) int) exitMatcher {
	return func(err error) (int, bool) {
		var target E
		if !errors.As(err, &target) {
			return 0, false
		}
		code := fn(target)
		if !registrableExitCode(code) {
			return 0, false
		}
		return code, true
	}
}
