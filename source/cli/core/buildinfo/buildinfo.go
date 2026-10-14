package buildinfo

import (
	"encoding/json"
	"regexp"
	"runtime"
	"runtime/debug"
)

// The link-time values. The linker sets them with -X only while they are uninitialised string
// variables that the program references; see the package documentation.
var (
	version string //nolint:gochecknoglobals // -X sets only a package-level string variable
	commit  string //nolint:gochecknoglobals // -X sets only a package-level string variable
	date    string //nolint:gochecknoglobals // -X sets only a package-level string variable
)

// schema is the value of the "schema" key in the JSON encoding of an [Info].
const schema = 1

// shortCommitLen is the number of characters of the commit that [Info.String] prints.
const shortCommitLen = 12

// releaseVersion matches vMAJOR.MINOR.PATCH with an optional prerelease and no build metadata.
var releaseVersion = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`,
)

// pseudoVersionTail matches the timestamp and revision a Go pseudo-version ends with, in each of
// its forms: vX.0.0-yyyymmddhhmmss-abcdefabcdef, vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef and
// vX.Y.Z-0.yyyymmddhhmmss-abcdefabcdef.
var pseudoVersionTail = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}$`)

// Info describes a build of a binary. A field is "" when nothing supplies its value.
type Info struct {
	// Version is the binary's version, such as "v1.0.0" or a Go pseudo-version.
	Version string
	// Commit is the full revision the binary was built from.
	Commit string
	// Date is the time of the build or of the commit, as it was supplied. It is never parsed.
	Date string
	// GoVersion is the version of the Go toolchain that built the binary, such as "go1.27.2".
	GoVersion string
	// OS is the operating system the binary was built for, as [runtime.GOOS] names it.
	OS string
	// Arch is the architecture the binary was built for, as [runtime.GOARCH] names it.
	Arch string
}

// ldflags holds the values the linker sets with -X.
type ldflags struct {
	version string
	commit  string
	date    string
}

// Read returns the Info of the running binary, resolved as the package documentation describes.
func Read() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(ldflags{version: version, commit: commit, date: date}, bi, ok)
}

// resolve merges the link-time values with the build information, field by field: a non-empty
// link-time value wins, and the build information is consulted only when ok is true. The
// toolchain, operating system and architecture always come from the runtime.
func resolve(lf ldflags, bi *debug.BuildInfo, ok bool) Info {
	info := Info{
		Version:   lf.version,
		Commit:    lf.commit,
		Date:      lf.date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if !ok || bi == nil {
		return info
	}
	if info.Version == "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && info.Commit == "":
			info.Commit = s.Value
		case s.Key == "vcs.time" && info.Date == "":
			info.Date = s.Value
		}
	}
	return info
}

// IsDev reports whether the build is not a release. A release version is vMAJOR.MINOR.PATCH with
// an optional prerelease, such as "v1.2.3" or "v1.2.3-rc.1", that is not a Go pseudo-version.
// An empty version, "(devel)", a version carrying build metadata such as "v1.2.3+dirty", and
// every pseudo-version are dev.
func (i Info) IsDev() bool {
	return !releaseVersion.MatchString(i.Version) || pseudoVersionTail.MatchString(i.Version)
}

// String returns the one-line text form: the version, or "dev" when it is empty, followed by
// " build " and the first 12 characters of the commit when the commit is not empty.
func (i Info) String() string {
	s := i.Version
	if s == "" {
		s = "dev"
	}
	if i.Commit == "" {
		return s
	}
	return s + " build " + i.Commit[:min(len(i.Commit), shortCommitLen)]
}

// jsonInfo is the JSON encoding of an Info. Its keys are the package's JSON contract.
type jsonInfo struct {
	Schema    int    `json:"schema"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Dev       bool   `json:"dev"`
}

// MarshalJSON encodes i as the JSON object the package documentation specifies, with the
// "schema" key, the full commit and the "dev" key set to [Info.IsDev]. Every key is present
// whatever i holds.
func (i Info) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonInfo{
		Schema:    schema,
		Version:   i.Version,
		Commit:    i.Commit,
		Date:      i.Date,
		GoVersion: i.GoVersion,
		OS:        i.OS,
		Arch:      i.Arch,
		Dev:       i.IsDev(),
	})
}
