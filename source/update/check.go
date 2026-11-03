package update

import (
	"context"
	"errors"
	"time"

	"go.vipengele.org/cli/spec/version"
)

// checkInputs is everything that decides whether a background update check runs, read once from
// a resolved [Config] and the stored [State] so that [eligibility] is a pure function of it.
type checkInputs struct {
	// Version is the running program's version.
	Version string
	// InCI reports that the CI environment variable is set to a non-empty value.
	InCI bool
	// Terminal reports that stderr is a terminal.
	Terminal bool
	// Disabled reports that the tool turned the check off through [Config.DisableCheck].
	Disabled bool
	// OptedOut reports that the user turned the check off through the opt-out variable.
	OptedOut bool
	// Interval is the minimum time between check attempts.
	Interval time.Duration
	// LastAttempt is when the last check was made; the zero time means never.
	LastAttempt time.Time
	// Now is the current time.
	Now time.Time
}

// checkInputsFrom reads the inputs of [eligibility] from r, a Config [Config.resolve] returned,
// and st, the stored State. Every input is read through r's seams: the environment through
// Getenv, the terminal through IsTerminal and the clock through Now. The opt-out variable is
// Identity.EnvVar(optOutSuffix), set to any non-empty value, "0" included.
func checkInputsFrom(r Config, st State) checkInputs {
	return checkInputs{
		Version:     r.Version,
		InCI:        r.Getenv("CI") != "",
		Terminal:    r.IsTerminal(),
		Disabled:    r.DisableCheck,
		OptedOut:    optedOut(r),
		Interval:    r.CheckInterval,
		LastAttempt: st.LastAttempt,
		Now:         r.Now(),
	}
}

// optedOut reports whether the opt-out variable of r's Identity is set to a non-empty value. An
// EnvPrefix that names no valid variable leaves the user no variable to set, so it is never opted
// out; a Config that passed [Config.Validate] always has a valid one.
func optedOut(r Config) bool {
	name, err := r.EnvVar(optOutSuffix)
	if err != nil {
		return false
	}
	return r.Getenv(name) != ""
}

// checkReason is why a background update check runs or does not.
type checkReason int

const (
	// checkEligible means every gate passed and the check runs.
	checkEligible checkReason = iota
	// ineligibleDevBuild means the running version is not a release version.
	ineligibleDevBuild
	// ineligibleCI means the CI environment variable is set.
	ineligibleCI
	// ineligibleNoTerminal means stderr is not a terminal.
	ineligibleNoTerminal
	// ineligibleDisabled means the tool turned the check off.
	ineligibleDisabled
	// ineligibleOptedOut means the user turned the check off.
	ineligibleOptedOut
	// ineligibleThrottled means the last attempt is more recent than the check interval.
	ineligibleThrottled
)

// String returns the constant's name.
func (r checkReason) String() string {
	switch r {
	case checkEligible:
		return "checkEligible"
	case ineligibleDevBuild:
		return "ineligibleDevBuild"
	case ineligibleCI:
		return "ineligibleCI"
	case ineligibleNoTerminal:
		return "ineligibleNoTerminal"
	case ineligibleDisabled:
		return "ineligibleDisabled"
	case ineligibleOptedOut:
		return "ineligibleOptedOut"
	case ineligibleThrottled:
		return "ineligibleThrottled"
	default:
		return "checkReason(unknown)"
	}
}

// eligibility returns the first gate that fails, in the order of the [checkReason]
// constants, or checkEligible when none does. A version is a dev build when
// [version.IsRelease] rejects it. The throttle passes when no attempt is recorded, when the
// recorded attempt lies after Now because the clock moved back, or when at least Interval has
// elapsed since it; a clock moved back would otherwise suppress every check until it caught up.
func eligibility(in checkInputs) checkReason {
	switch {
	case !version.IsRelease(in.Version):
		return ineligibleDevBuild
	case in.InCI:
		return ineligibleCI
	case !in.Terminal:
		return ineligibleNoTerminal
	case in.Disabled:
		return ineligibleDisabled
	case in.OptedOut:
		return ineligibleOptedOut
	case !throttleElapsed(in.LastAttempt, in.Now, in.Interval):
		return ineligibleThrottled
	default:
		return checkEligible
	}
}

// throttleElapsed reports whether a check may run at now given the last attempt and the interval.
func throttleElapsed(last, now time.Time, interval time.Duration) bool {
	return last.IsZero() || last.After(now) || now.Sub(last) >= interval
}

// afterAttempt returns the State to store once a check that ran under ctx finished at now with
// latest and err, and whether to store it at all.
//
// A check stopped because the command ended is not an attempt: when ctx is done, cancelled or
// past its deadline, or err is [context.Canceled], st is returned unchanged with false, so the
// stopped check neither throttles the next run nor erases what is known. The error alone cannot
// tell: a deadline on ctx surfaces as a [*TimeoutError], the same error as the source's own
// per-request timeout, so ctx itself is consulted. A source request that timed out while ctx is
// still live is an attempt.
//
// Any attempt sets LastAttempt to now in UTC and returns true. LatestVersion becomes latest only
// when err is nil; a failed attempt keeps the LatestVersion st already holds.
func afterAttempt(ctx context.Context, st State, now time.Time, latest string, err error) (State, bool) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return st, false
	}
	st.LastAttempt = now.UTC()
	if err == nil {
		st.LatestVersion = latest
	}
	return st, true
}
