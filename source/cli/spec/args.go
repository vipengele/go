package spec

import (
	"fmt"

	"go.vipengele.org/commons/validate"
)

// ArgPolicy is the number of positional arguments a [Command] accepts. It is built by [NoArgs],
// [AnyArgs], [ExactArgs] or [RangeArgs]; the zero value is [NoArgs]. An adapter evaluates it
// with [ArgPolicy.Check] and may read its bounds with [ArgPolicy.Min] and [ArgPolicy.Max].
type ArgPolicy struct {
	min       int
	max       int
	unbounded bool
}

// NoArgs accepts no positional argument. It equals the zero value of [ArgPolicy].
func NoArgs() ArgPolicy {
	return ArgPolicy{}
}

// AnyArgs accepts any number of positional arguments, none included.
func AnyArgs() ArgPolicy {
	return ArgPolicy{unbounded: true}
}

// ExactArgs accepts exactly n positional arguments. A negative n is reported by
// [ArgPolicy.Validate].
func ExactArgs(n int) ArgPolicy {
	return ArgPolicy{min: n, max: n}
}

// RangeArgs accepts between minArgs and maxArgs positional arguments, both included. A negative
// bound or minArgs greater than maxArgs is reported by [ArgPolicy.Validate].
func RangeArgs(minArgs, maxArgs int) ArgPolicy {
	return ArgPolicy{min: minArgs, max: maxArgs}
}

// Min returns the fewest positional arguments the policy accepts.
func (p ArgPolicy) Min() int {
	return p.min
}

// Max returns the most positional arguments the policy accepts, and false when it accepts any
// number above [ArgPolicy.Min].
func (p ArgPolicy) Max() (int, bool) {
	if p.unbounded {
		return 0, false
	}
	return p.max, true
}

// Validate returns nil for a policy whose bounds are non-negative and ordered. Otherwise it
// returns an error wrapping [validate.ErrInvalid].
func (p ArgPolicy) Validate() error {
	switch {
	case p.min < 0:
		return fmt.Errorf("%w: argument policy minimum %d is negative", validate.ErrInvalid, p.min)
	case p.unbounded:
		return nil
	case p.max < 0:
		return fmt.Errorf("%w: argument policy maximum %d is negative", validate.ErrInvalid, p.max)
	case p.min > p.max:
		return fmt.Errorf("%w: argument policy minimum %d exceeds its maximum %d",
			validate.ErrInvalid, p.min, p.max)
	}
	return nil
}

// Check returns nil when the policy accepts args. It returns an [*ArgCountError] when the number
// of arguments is out of bounds, and the [ArgPolicy.Validate] error when the policy itself is
// invalid. It never inspects the arguments' values.
func (p ArgPolicy) Check(args []string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	n := len(args)
	if n < p.min || (!p.unbounded && n > p.max) {
		return &ArgCountError{Policy: p, Got: n}
	}
	return nil
}

// String describes the arguments the policy accepts, as in "exactly 2 arguments".
func (p ArgPolicy) String() string {
	switch {
	case p.unbounded && p.min == 0:
		return "any number of arguments"
	case p.unbounded:
		return "at least " + countArgs(p.min)
	case p.max == 0 && p.min == 0:
		return "no arguments"
	case p.min == p.max:
		return "exactly " + countArgs(p.min)
	}
	return fmt.Sprintf("between %d and %d arguments", p.min, p.max)
}

// ArgCountError reports positional arguments whose number a policy does not accept.
type ArgCountError struct {
	// Policy is the policy that rejected the arguments.
	Policy ArgPolicy
	// Got is the number of arguments received.
	Got int
}

// Error states what the policy accepts and how many arguments were received.
func (e *ArgCountError) Error() string {
	return fmt.Sprintf("accepts %s, received %s", e.Policy, countArgs(e.Got))
}

func countArgs(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}
