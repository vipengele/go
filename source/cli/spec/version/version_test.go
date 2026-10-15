package version

import (
	"strings"
	"testing"
)

func TestIsReleaseClassifiesVersions(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"", false},
		{"(devel)", false},
		{"v1.2.3", true},
		{"v0.0.0", true},
		{"v10.20.30", true},
		{"v1.2.3-rc.1", true},
		{"v1.2.3-alpha", true},
		{"v1.2.3-0.beta-1", true},
		{"v1.2.3+dirty", false},
		{"v1.2.3-rc.1+dirty", false},
		{"v1.2.3+incompatible", false},
		{"v0.0.0-20260102030405-0123abcdef01", false},
		{"v1.2.4-0.20260102030405-0123abcdef01", false},
		{"v1.2.3-pre.0.20260102030405-0123abcdef01", false},
		{"v0.0.0-20260102030405-0123abcdef01+dirty", false},
		{"1.2.3", false},
		{"v1.2", false},
		{"v1", false},
		{"v01.2.3", false},
		{"v1.2.3-", false},
		{"v1.2.3-rc..1", false},
		{"dev", false},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			if got := IsRelease(tc.version); got != tc.want {
				t.Errorf("IsRelease(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestNewerOrdersReleaseVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"patch above", "v1.2.3", "v1.2.4", true},
		{"minor above", "v1.2.3", "v1.3.0", true},
		{"major above", "v1.2.3", "v2.0.0", true},
		{"minor compared numerically", "v1.9.0", "v1.10.0", true},
		{"minor compared numerically, reversed", "v1.10.0", "v1.9.0", false},
		{"equal versions", "v1.2.3", "v1.2.3", false},
		{"equal prereleases", "v1.2.3-rc.1", "v1.2.3-rc.1", false},
		{"current newer than latest", "v1.3.0", "v1.2.9", false},
		{"release above its prerelease", "v1.2.3-rc.1", "v1.2.3", true},
		{"prerelease below its release", "v1.2.3", "v1.2.3-rc.1", false},
		{"rc.2 above rc.1", "v1.2.3-rc.1", "v1.2.3-rc.2", true},
		{"rc.1 below rc.2", "v1.2.3-rc.2", "v1.2.3-rc.1", false},
		{"prerelease of a later version above a stable current", "v1.2.3", "v1.3.0-rc.1", true},
		{"prerelease of an earlier version below a stable current", "v1.2.3", "v1.2.0-rc.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Newer(tc.current, tc.latest); got != tc.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestNewerTreatsDevBuildAsOlderThanEveryRelease(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
	}{
		{"empty current", "", "v0.0.0"},
		{"(devel) current", "(devel)", "v1.2.3"},
		{"dev current", "dev", "v1.2.3"},
		{"pseudo-version current", "v1.2.4-0.20260102030405-0123abcdef01", "v1.2.3"},
		{"pseudo-version current above a prerelease", "v9.0.1-0.20260102030405-0123abcdef01", "v0.0.1-rc.1"},
		{"build metadata on current", "v1.2.3+dirty", "v1.2.3"},
		{"build metadata on a newer current", "v2.0.0+dirty", "v1.0.0"},
		{"shorthand current", "v2", "v1.0.0"},
		{"current without v", "2.0.0", "v1.0.0"},
		{"malformed current", "v1.2.3-rc..1", "v1.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !Newer(tc.current, tc.latest) {
				t.Errorf("Newer(%q, %q) = false, want true", tc.current, tc.latest)
			}
		})
	}
}

func TestNewerRejectsLatestThatIsNotARelease(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
	}{
		{"empty latest", "v1.2.3", ""},
		{"both empty", "", ""},
		{"(devel) latest", "v1.2.3", "(devel)"},
		{"(devel) on both sides", "(devel)", "(devel)"},
		{"dev latest over dev current", "dev", "dev"},
		{"shorthand latest", "v1.2.3", "v2"},
		{"minor shorthand latest", "v1.2.3", "v1.3"},
		{"latest without v", "v1.2.3", "1.3.0"},
		{"latest without v over dev current", "", "1.3.0"},
		{"build metadata on latest", "v1.2.3", "v1.3.0+dirty"},
		{"incompatible latest", "v1.2.3", "v2.0.0+incompatible"},
		{"pseudo-version latest", "v1.2.3", "v1.2.4-0.20260102030405-0123abcdef01"},
		{"pseudo-version latest over dev current", "(devel)", "v0.0.0-20260102030405-0123abcdef01"},
		{"malformed latest", "v1.2.3", "v1.3.0-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if Newer(tc.current, tc.latest) {
				t.Errorf("Newer(%q, %q) = true, want false", tc.current, tc.latest)
			}
		})
	}
}

func TestNewerAndIsReleaseDoNotPanicOnHostileInput(t *testing.T) {
	hostile := []string{
		"",
		"v",
		"vv1.2.3",
		"v1.2.3\x00",
		"\x00v1.2.3",
		"v1.2.3-\x00",
		"v１.２.３",
		"v1.2.3-ρc.1",
		"v1.2.3-rc.1+\u202e",
		"\xff\xfe\xfd",
		"v" + strings.Repeat("9", 10000) + ".0.0",
		"v1.2.3-" + strings.Repeat("a.", 10000) + "a",
		strings.Repeat("v1.2.3", 10000),
		"v99999999999999999999999999999999.0.0",
		"v1.2.3-" + strings.Repeat("0", 5000),
	}
	for _, s := range hostile {
		IsRelease(s)
		for _, other := range []string{"", "(devel)", "v1.2.3", s} {
			Newer(other, s)
			Newer(s, other)
		}
	}
}
