package buildinfo

import (
	"bytes"
	"encoding/json"
	"runtime"
	"runtime/debug"
	"testing"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/speccheck"
)

const (
	testSHA  = "0123abcdef0123abcdef0123abcdef0123abcdef"
	testTime = "2026-01-02T03:04:05Z"
)

func buildInfo(mainVersion string, vcs bool) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Path: "example.com/tool", Version: mainVersion}}
	if vcs {
		bi.Settings = []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: testSHA},
			{Key: "vcs.time", Value: testTime},
			{Key: "vcs.modified", Value: "false"},
		}
	}
	return bi
}

func withRuntime(info Info) Info {
	info.GoVersion = runtime.Version()
	info.OS = runtime.GOOS
	info.Arch = runtime.GOARCH
	return info
}

func TestResolveLdflagsOnly(t *testing.T) {
	lf := ldflags{version: "v1.0.0", commit: "abc123", date: "2025-12-31"}
	want := withRuntime(Info{Version: "v1.0.0", Commit: "abc123", Date: "2025-12-31"})

	tests := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
	}{
		{"no build information", nil, false},
		{"nil build information reported as present", nil, true},
		{"build information without vcs settings", buildInfo("v9.9.9", false), true},
		{"build information with vcs settings", buildInfo("v9.9.9", true), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(lf, tc.bi, tc.ok); got != want {
				t.Errorf("resolve() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveBuildInfoOnly(t *testing.T) {
	tests := []struct {
		name        string
		mainVersion string
		wantVersion string
	}{
		{"devel", "(devel)", ""},
		{"pseudo-version", "v0.0.0-20260102030405-0123abcdef01", "v0.0.0-20260102030405-0123abcdef01"},
		{"prerelease pseudo-version", "v1.2.3-pre.0.20260102030405-0123abcdef01", "v1.2.3-pre.0.20260102030405-0123abcdef01"},
		{"release", "v1.2.3", "v1.2.3"},
		{"dirty", "v1.2.3+dirty", "v1.2.3+dirty"},
	}
	for _, tc := range tests {
		for _, vcs := range []bool{false, true} {
			name := tc.name + " without vcs settings"
			want := withRuntime(Info{Version: tc.wantVersion})
			if vcs {
				name = tc.name + " with vcs settings"
				want.Commit = testSHA
				want.Date = testTime
			}
			t.Run(name, func(t *testing.T) {
				if got := resolve(ldflags{}, buildInfo(tc.mainVersion, vcs), true); got != want {
					t.Errorf("resolve() = %#v, want %#v", got, want)
				}
			})
		}
	}
}

func TestResolveIgnoresBuildInfoNotReported(t *testing.T) {
	want := withRuntime(Info{})
	if got := resolve(ldflags{}, buildInfo("v1.2.3", true), false); got != want {
		t.Errorf("resolve() = %#v, want %#v", got, want)
	}
}

func TestResolvePrecedenceIsPerField(t *testing.T) {
	tests := []struct {
		name string
		lf   ldflags
		want Info
	}{
		{
			"version from ldflags",
			ldflags{version: "v2.0.0"},
			Info{Version: "v2.0.0", Commit: testSHA, Date: testTime},
		},
		{
			"commit from ldflags",
			ldflags{commit: "fedcba"},
			Info{Version: "v1.2.3", Commit: "fedcba", Date: testTime},
		},
		{
			"date from ldflags",
			ldflags{date: "2025-12-31"},
			Info{Version: "v1.2.3", Commit: testSHA, Date: "2025-12-31"},
		},
		{
			"version and date from ldflags",
			ldflags{version: "v2.0.0", date: "2025-12-31"},
			Info{Version: "v2.0.0", Commit: testSHA, Date: "2025-12-31"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := withRuntime(tc.want)
			if got := resolve(tc.lf, buildInfo("v1.2.3", true), true); got != want {
				t.Errorf("resolve() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveLdflagsVersionReplacesDevel(t *testing.T) {
	want := withRuntime(Info{Version: "v1.0.0"})
	if got := resolve(ldflags{version: "v1.0.0"}, buildInfo("(devel)", false), true); got != want {
		t.Errorf("resolve() = %#v, want %#v", got, want)
	}
}

func TestReadReportsRuntime(t *testing.T) {
	got := Read()
	if got.GoVersion != runtime.Version() || got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Errorf("Read() = %#v, want GoVersion %q, OS %q, Arch %q",
			got, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
}

func TestInfoIsDev(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"", true},
		{"(devel)", true},
		{"v1.2.3", false},
		{"v1.2.3-rc.1", false},
		{"v1.2.3+dirty", true},
		{"v0.0.0-20260102030405-0123abcdef01", true},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			if got := (Info{Version: tc.version}).IsDev(); got != tc.want {
				t.Errorf("Info{Version: %q}.IsDev() = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestInfoString(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{"version and commit", Info{Version: "v1.0.0", Commit: testSHA}, "v1.0.0 build 0123abcdef01"},
		{"version only", Info{Version: "v1.0.0"}, "v1.0.0"},
		{"commit only", Info{Commit: testSHA}, "dev build 0123abcdef01"},
		{"nothing known", Info{}, "dev"},
		{"short commit", Info{Version: "v1.0.0", Commit: "abc123"}, "v1.0.0 build abc123"},
		{"commit of exactly 12 characters", Info{Version: "v1.0.0", Commit: "0123abcdef01"}, "v1.0.0 build 0123abcdef01"},
		{
			"pseudo-version",
			Info{Version: "v0.0.0-20260102030405-0123abcdef01", Commit: testSHA},
			"v0.0.0-20260102030405-0123abcdef01 build 0123abcdef01",
		},
		{"dirty", Info{Version: "v1.2.3+dirty"}, "v1.2.3+dirty"},
		{"date and runtime are not printed", Info{Version: "v1.0.0", Date: testTime, GoVersion: "go1.27.2", OS: "linux", Arch: "amd64"}, "v1.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInfoMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			"release",
			Info{Version: "v1.0.0", Commit: testSHA, Date: testTime, GoVersion: "go1.27.2", OS: "linux", Arch: "amd64"},
			`{"version":"v1.0.0","commit":"` + testSHA + `","date":"` + testTime +
				`","go":"go1.27.2","os":"linux","arch":"amd64","dev":false}`,
		},
		{
			"zero value keeps every key",
			Info{},
			`{"version":"","commit":"","date":"","go":"","os":"","arch":"","dev":true}`,
		},
		{
			"pseudo-version",
			Info{Version: "v0.0.0-20260102030405-0123abcdef01"},
			`{"version":"v0.0.0-20260102030405-0123abcdef01","commit":"","date":"","go":"","os":"","arch":"","dev":true}`,
		},
		{
			"date is passed through unvalidated",
			Info{Version: "v1.0.0", Date: "not a date"},
			`{"version":"v1.0.0","commit":"","date":"not a date","go":"","os":"","arch":"","dev":false}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.info)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestInfoMarshalJSONThroughPointer(t *testing.T) {
	info := &Info{Version: "v1.0.0"}
	got, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"version":"v1.0.0","commit":"","date":"","go":"","os":"","arch":"","dev":false}`
	if string(got) != want {
		t.Errorf("json.Marshal() = %s, want %s", got, want)
	}
}

// TestVersionJSONKeys pins the keys of the object "version --json" writes, the Info under a
// schema 1 envelope. A key renamed, removed or retyped here is a contract change that increments
// schema (ADR-0007).
func TestVersionJSONKeys(t *testing.T) {
	var out bytes.Buffer
	info := Info{Version: "v1.0.0", Commit: testSHA, Date: testTime, GoVersion: "go1.27.2", OS: "linux", Arch: "amd64"}
	if err := spec.WriteJSON(&out, 1, info); err != nil {
		t.Fatalf("spec.WriteJSON() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", out.Bytes(), err)
	}
	if got["schema"] != float64(1) {
		t.Errorf("schema = %v, want 1", got["schema"])
	}
	speccheck.AssertJSONKeys(t, got, "testdata/version-json.golden")
}
