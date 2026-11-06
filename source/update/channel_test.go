package update

import (
	"strings"
	"testing"
)

func TestChannelOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  string
		want Channel
	}{
		{"v1.2.3", Stable},
		{"v1.2.3-beta.1", Beta},
		{"v1.2.3-beta", Beta},
		{"v1.2.3-rc.2", Beta},
		{"v1.2.3-edge.147", Edge},
		{"v1.2.3-alpha.1", Edge},
		{"v1.2.3-dev.3", Edge},
		{"v1.2.3-betamax.1", Edge},
	}
	for _, tt := range tests {
		for _, flag := range []bool{false, true} {
			want := tt.want
			if flag && want < Beta {
				want = Beta
			}
			if got := channelOf(tt.tag, flag); got != want {
				t.Errorf("channelOf(%q, %t) = %v, want %v", tt.tag, flag, got, want)
			}
		}
	}
}

func TestChannelReceives(t *testing.T) {
	t.Parallel()

	for c := Stable; c <= Edge; c++ {
		for r := Stable; r <= Edge; r++ {
			if got, want := c.Receives(r), r <= c; got != want {
				t.Errorf("%v.Receives(%v) = %t, want %t", c, r, got, want)
			}
		}
	}
	for _, bad := range []Channel{-1, 3} {
		if Edge.Receives(bad) || bad.Receives(Stable) {
			t.Errorf("an invalid channel %v receives or is received", bad)
		}
	}
}

func TestChannelString(t *testing.T) {
	t.Parallel()

	for c, want := range map[Channel]string{Stable: "stable", Beta: "beta", Edge: "edge", 7: "Channel(7)"} {
		if got := c.String(); got != want {
			t.Errorf("Channel(%d).String() = %q, want %q", int(c), got, want)
		}
	}
	var zero Channel
	if zero != Stable {
		t.Errorf("zero Channel = %v, want stable", zero)
	}
}

func TestParseChannel(t *testing.T) {
	t.Parallel()

	for _, want := range []Channel{Stable, Beta, Edge} {
		got, err := ParseChannel(want.String())
		if err != nil || got != want {
			t.Errorf("ParseChannel(%q) = %v, %v, want %v", want.String(), got, err, want)
		}
	}
	for _, in := range []string{"", "Stable", "BETA", "nightly", " edge"} {
		_, err := ParseChannel(in)
		if err == nil {
			t.Errorf("ParseChannel(%q) succeeded, want an error", in)
			continue
		}
		for _, name := range []string{"stable", "beta", "edge"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("ParseChannel(%q) error %q does not mention %q", in, err, name)
			}
		}
	}
}

func TestChannelTextRoundTrip(t *testing.T) {
	t.Parallel()

	for _, c := range []Channel{Stable, Beta, Edge} {
		text, err := c.MarshalText()
		if err != nil {
			t.Fatalf("%v.MarshalText: %v", c, err)
		}
		got := Edge
		if c == Edge {
			got = Stable
		}
		if err := got.UnmarshalText(text); err != nil || got != c {
			t.Errorf("round trip of %v = %v, %v", c, got, err)
		}
	}
	if _, err := Channel(9).MarshalText(); err == nil {
		t.Error("MarshalText of an invalid channel succeeded")
	}
	c := Beta
	if err := c.UnmarshalText([]byte("nope")); err == nil || c != Beta {
		t.Errorf("UnmarshalText(nope) = %v, channel %v; want an error and no change", err, c)
	}
}
