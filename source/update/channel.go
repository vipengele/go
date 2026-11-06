package update

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Channel is a stream of releases an installation follows. Channels are cumulative: a channel
// receives every release whose own channel is at or below it, so Edge receives all releases and
// Stable only the stable ones. The zero value is Stable.
type Channel int

const (
	// Stable receives releases whose tag has no pre-release suffix.
	Stable Channel = iota
	// Beta receives Stable releases and pre-releases tagged beta or rc, or marked as a
	// pre-release by the source.
	Beta
	// Edge receives every release, including pre-releases with any other suffix.
	Edge
)

// String returns the lower-case name of c: "stable", "beta" or "edge". A value outside the
// defined channels renders as "Channel(n)".
func (c Channel) String() string {
	switch c {
	case Stable:
		return "stable"
	case Beta:
		return "beta"
	case Edge:
		return "edge"
	}
	return fmt.Sprintf("Channel(%d)", int(c))
}

// valid reports whether c is one of Stable, Beta and Edge.
func (c Channel) valid() bool { return c >= Stable && c <= Edge }

// ParseChannel returns the Channel named s, one of "stable", "beta" and "edge". Any other s,
// including a different letter case, is an error naming the valid values.
func ParseChannel(s string) (Channel, error) {
	switch s {
	case "stable":
		return Stable, nil
	case "beta":
		return Beta, nil
	case "edge":
		return Edge, nil
	}
	return Stable, fmt.Errorf("update: unknown channel %q: want stable, beta or edge", s)
}

// Receives reports whether a follower of c receives a release on channel r, which holds when r
// is at or below c. It is false when either channel is not a defined one.
func (c Channel) Receives(r Channel) bool {
	return c.valid() && r.valid() && r <= c
}

// MarshalText implements [encoding.TextMarshaler] with the name String returns. It fails for a
// value that is not a defined channel.
func (c Channel) MarshalText() ([]byte, error) {
	if !c.valid() {
		return nil, fmt.Errorf("update: cannot marshal %s", c)
	}
	return []byte(c.String()), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler] through [ParseChannel]. c is unchanged
// when it fails.
func (c *Channel) UnmarshalText(text []byte) error {
	parsed, err := ParseChannel(string(text))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// channelOf returns the channel of a release: the higher of the channel its tag implies and the
// channel its prerelease flag implies.
//
// A tag with no pre-release suffix implies Stable; one whose suffix starts with a beta or rc
// identifier, up to the first dot, implies Beta; any other suffix implies Edge. The flag, the
// source's own pre-release marking, implies at least Beta.
//
// It assumes tag is a release version as [version.IsRelease] defines it. A tag semver does not
// parse has no suffix and is Stable, so callers drop such releases before asking.
func channelOf(tag string, prerelease bool) Channel {
	c := Stable
	if pre := strings.TrimPrefix(semver.Prerelease(tag), "-"); pre != "" {
		id, _, _ := strings.Cut(pre, ".")
		switch id {
		case "beta", "rc":
			c = Beta
		default:
			c = Edge
		}
	}
	if prerelease && c < Beta {
		c = Beta
	}
	return c
}
