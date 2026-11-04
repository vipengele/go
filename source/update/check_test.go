package update

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// eligibleInputs passes every gate: a release version, outside CI, on a terminal, enabled, not
// opted out, and with no attempt recorded.
func eligibleInputs() checkInputs {
	return checkInputs{
		Version:  "v1.2.3",
		Terminal: true,
		Interval: 24 * time.Hour,
		Now:      time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestEligibility(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		modify func(*checkInputs)
		want   checkReason
	}{
		{"every gate passes", func(*checkInputs) {}, checkEligible},
		{"empty version is a dev build", func(in *checkInputs) { in.Version = "" }, ineligibleDevBuild},
		{"dev version is a dev build", func(in *checkInputs) { in.Version = "dev" }, ineligibleDevBuild},
		{"pseudo-version is a dev build", func(in *checkInputs) { in.Version = "v0.0.0-20260101000000-abcdef123456" }, ineligibleDevBuild},
		{"build metadata is a dev build", func(in *checkInputs) { in.Version = "v1.2.3+dirty" }, ineligibleDevBuild},
		{"prerelease version passes", func(in *checkInputs) { in.Version = "v1.2.3-rc.1" }, checkEligible},
		{"CI blocks", func(in *checkInputs) { in.InCI = true }, ineligibleCI},
		{"no terminal blocks", func(in *checkInputs) { in.Terminal = false }, ineligibleNoTerminal},
		{"disabled blocks", func(in *checkInputs) { in.Disabled = true }, ineligibleDisabled},
		{"opted out blocks", func(in *checkInputs) { in.OptedOut = true }, ineligibleOptedOut},
		{"attempt inside the interval throttles", func(in *checkInputs) { in.LastAttempt = now.Add(-time.Hour) }, ineligibleThrottled},
		{"elapsed equal to the interval passes", func(in *checkInputs) { in.LastAttempt = now.Add(-in.Interval) }, checkEligible},
		{"elapsed 1ns short of the interval throttles", func(in *checkInputs) { in.LastAttempt = now.Add(-in.Interval + time.Nanosecond) }, ineligibleThrottled},
		{"zero last attempt passes", func(in *checkInputs) { in.LastAttempt = time.Time{} }, checkEligible},
		{"last attempt in the future passes", func(in *checkInputs) { in.LastAttempt = now.Add(time.Hour) }, checkEligible},
		{"dev build is reported before every other gate", func(in *checkInputs) {
			in.Version = "dev"
			in.InCI = true
			in.Terminal = false
			in.Disabled = true
			in.OptedOut = true
			in.LastAttempt = now
		}, ineligibleDevBuild},
		{"CI is reported before the later gates", func(in *checkInputs) {
			in.InCI = true
			in.Terminal = false
			in.Disabled = true
			in.OptedOut = true
			in.LastAttempt = now
		}, ineligibleCI},
		{"no terminal is reported before the later gates", func(in *checkInputs) {
			in.Terminal = false
			in.Disabled = true
			in.OptedOut = true
			in.LastAttempt = now
		}, ineligibleNoTerminal},
		{"disabled is reported before opt-out and throttle", func(in *checkInputs) {
			in.Disabled = true
			in.OptedOut = true
			in.LastAttempt = now
		}, ineligibleDisabled},
		{"opt-out is reported before throttle", func(in *checkInputs) {
			in.OptedOut = true
			in.LastAttempt = now
		}, ineligibleOptedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := eligibleInputs()
			tt.modify(&in)
			if got := eligibility(in); got != tt.want {
				t.Errorf("eligibility(%+v) = %v, want %v", in, got, tt.want)
			}
		})
	}
}

func TestCheckInputsFromReadsEveryInputThroughTheConfig(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.FixedZone("X", 3600))
	last := time.Date(2026, 5, 31, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		env      map[string]string
		terminal bool
		disabled bool
		want     checkInputs
	}{
		{
			name:     "nothing set",
			terminal: true,
			want:     checkInputs{Terminal: true},
		},
		{
			name:     "CI and opt-out set, no terminal, disabled",
			env:      map[string]string{"CI": "true", "ATK_NO_UPDATE_CHECK": "1"},
			disabled: true,
			want:     checkInputs{InCI: true, Disabled: true, OptedOut: true},
		},
		{
			name:     "opt-out set to 0 still opts out",
			env:      map[string]string{"ATK_NO_UPDATE_CHECK": "0"},
			terminal: true,
			want:     checkInputs{Terminal: true, OptedOut: true},
		},
		{
			name:     "variables set to empty count as unset",
			env:      map[string]string{"CI": "", "ATK_NO_UPDATE_CHECK": ""},
			terminal: true,
			want:     checkInputs{Terminal: true},
		},
		{
			name:     "opt-out of another prefix is ignored",
			env:      map[string]string{"NO_UPDATE_CHECK": "1", "OTHER_NO_UPDATE_CHECK": "1"},
			terminal: true,
			want:     checkInputs{Terminal: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := validConfig(t.TempDir())
			c.Version = "v1.2.3"
			c.CheckInterval = 3 * time.Hour
			c.DisableCheck = tt.disabled
			c.Getenv = envOf(tt.env)
			c.Now = func() time.Time { return now }
			c.IsTerminal = func() bool { return tt.terminal }

			got := checkInputsFrom(c, State{LastAttempt: last, LatestVersion: "v9.0.0"})

			want := tt.want
			want.Version = "v1.2.3"
			want.Interval = 3 * time.Hour
			if got.Version != want.Version || got.InCI != want.InCI || got.Terminal != want.Terminal ||
				got.Disabled != want.Disabled || got.OptedOut != want.OptedOut || got.Interval != want.Interval {
				t.Errorf("checkInputsFrom() = %+v, want %+v", got, want)
			}
			if !got.LastAttempt.Equal(last) {
				t.Errorf("LastAttempt = %v, want %v", got.LastAttempt, last)
			}
			if !got.Now.Equal(now) {
				t.Errorf("Now = %v, want %v", got.Now, now)
			}
		})
	}
}

func TestCheckInputsFromIsNotOptedOutWithoutAValidPrefix(t *testing.T) {
	t.Parallel()
	c := validConfig(t.TempDir())
	c.EnvPrefix = "not a prefix"
	c.Getenv = func(string) string { return "1" }
	c.Now = func() time.Time { return time.Time{} }
	c.IsTerminal = func() bool { return true }

	if got := checkInputsFrom(c, State{}); got.OptedOut {
		t.Error("OptedOut = true, want false for an EnvPrefix that names no variable")
	}
}

func TestAfterAttempt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.FixedZone("X", 3600))
	prev := State{
		LastAttempt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		LatestVersion: "v1.0.0",
	}
	cancelled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	expired := func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
		t.Cleanup(cancel)
		return ctx
	}
	live := context.Background
	sourceTimeout := &TimeoutError{URL: "https://example.test/x", Err: context.DeadlineExceeded}

	tests := []struct {
		name       string
		ctx        func() context.Context
		err        error
		wantSave   bool
		wantLatest string
	}{
		{"bare cancellation is not an attempt", live, context.Canceled, false, ""},
		{"wrapped cancellation is not an attempt", live, fmt.Errorf("update: GET: %w", context.Canceled), false, ""},
		{"success under a cancelled context is not an attempt", cancelled, nil, false, ""},
		{"failure under a cancelled context is not an attempt", cancelled, errors.New("boom"), false, ""},
		{"timeout under a cancelled context is not an attempt", cancelled, sourceTimeout, false, ""},
		{"success past the context deadline is not an attempt", expired, nil, false, ""},
		{"timeout past the context deadline is not an attempt", expired, &TimeoutError{URL: "https://example.test/x", Err: context.DeadlineExceeded}, false, ""},
		{"failure past the context deadline is not an attempt", expired, errors.New("boom"), false, ""},
		{"source timeout under a live context is an attempt", live, sourceTimeout, true, "v1.0.0"},
		{"other failure under a live context is an attempt", live, errors.New("boom"), true, "v1.0.0"},
		{"success records the latest version", live, nil, true, "v2.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, save := afterAttempt(tt.ctx(), prev, now, "v2.0.0", tt.err)
			if save != tt.wantSave {
				t.Fatalf("afterAttempt() save = %v, want %v", save, tt.wantSave)
			}
			if !tt.wantSave {
				if !got.LastAttempt.Equal(prev.LastAttempt) || got.LatestVersion != prev.LatestVersion {
					t.Errorf("afterAttempt() = %+v, want the state unchanged %+v", got, prev)
				}
				return
			}
			if !got.LastAttempt.Equal(now) {
				t.Errorf("LastAttempt = %v, want %v", got.LastAttempt, now)
			}
			if got.LastAttempt.Location() != time.UTC {
				t.Errorf("LastAttempt location = %v, want UTC", got.LastAttempt.Location())
			}
			if got.LatestVersion != tt.wantLatest {
				t.Errorf("LatestVersion = %q, want %q", got.LatestVersion, tt.wantLatest)
			}
		})
	}
}

func TestCheckReasonString(t *testing.T) {
	t.Parallel()
	tests := map[checkReason]string{
		checkEligible:        "checkEligible",
		ineligibleDevBuild:   "ineligibleDevBuild",
		ineligibleCI:         "ineligibleCI",
		ineligibleNoTerminal: "ineligibleNoTerminal",
		ineligibleDisabled:   "ineligibleDisabled",
		ineligibleOptedOut:   "ineligibleOptedOut",
		ineligibleThrottled:  "ineligibleThrottled",
		checkReason(-1):      "checkReason(unknown)",
	}
	for r, want := range tests {
		if got := r.String(); got != want {
			t.Errorf("checkReason(%d).String() = %q, want %q", int(r), got, want)
		}
	}
}
