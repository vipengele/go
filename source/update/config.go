package update

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/commons/validate"
)

// optOutSuffix is the suffix of the environment variable that turns update checks off. The
// variable's full name is Identity.EnvVar(optOutSuffix): "ATK_NO_UPDATE_CHECK" for EnvPrefix
// "ATK".
const optOutSuffix = "NO_UPDATE_CHECK"

// defaultAssetTemplate is the [Config.AssetTemplate] a Config without one uses. The library
// treats it as an opaque string; {name}, {os} and {arch} stand for the binary name and the
// target platform.
const defaultAssetTemplate = "{name}_{os}_{arch}.tar.gz"

// defaultChecksumsAsset is the [Config.ChecksumsAsset] a Config without one uses.
const defaultChecksumsAsset = "checksums.txt"

// tempPrefixSuffix follows the binary name in the default [Config.TempPrefix].
const tempPrefixSuffix = "-update-"

// ErrNoSource is reported by [Config.Validate] for a Config whose Source is nil: the one field a
// zero Config cannot default.
var ErrNoSource = errors.New("update: no release source: Config.Source is nil")

// Config is everything the update library needs to know about a tool, and every seam through
// which it reaches the outside world. Any field left at its zero value takes its production
// default, documented on the field, except Source: a nil Source cannot default, so
// [Config.Validate] reports it. The embedded [spec.Identity] is required in full.
//
// Config holds func fields, so it is not comparable.
type Config struct {
	spec.Identity

	// BinaryName is the name of the tool's binary. Empty means Identity.Name.
	BinaryName string
	// TempPrefix prefixes the names of the temporary files an update writes. Empty means
	// BinaryName followed by "-update-".
	TempPrefix string
	// AssetTemplate names the release asset that holds the binary for a platform. Empty means
	// "{name}_{os}_{arch}.tar.gz".
	AssetTemplate string
	// ChecksumsAsset is the name of the release asset that lists every asset's checksum. Empty
	// means "checksums.txt".
	ChecksumsAsset string
	// StateDir is the directory the library keeps its state in. Empty means the state directory
	// [paths.Resolve] resolves for the Identity, through the GOOS, Getenv and UserHomeDir seams: on
	// Linux $XDG_STATE_HOME/<DirName> or ~/.local/state/<DirName>, and <PREFIX>_HOME/state when
	// that variable is set. Only the state directory is resolved, so the home directory is needed
	// only when the state directory itself falls back to it. An explicit StateDir is used as given.
	StateDir string
	// Source reports the releases the tool can update to. It is required.
	Source Source

	// HTTPClient makes every HTTP request. Nil means a new [http.Client] of the Config's own,
	// never [http.DefaultClient].
	HTTPClient *http.Client
	// Now returns the current time. Nil means [time.Now].
	Now func() time.Time
	// GOOS is the target operating system. Empty means [runtime.GOOS]. It also selects the
	// directory layout the default StateDir is resolved for.
	GOOS string
	// GOARCH is the target architecture. Empty means [runtime.GOARCH].
	GOARCH string
	// Executable returns the path of the running binary. Nil means [os.Executable].
	Executable func() (string, error)
	// Getenv reads an environment variable. Nil means [os.Getenv]. The directory resolver reads
	// the environment through it too, and counts a variable whose value is "" as unset.
	Getenv func(string) string
	// UserHomeDir returns the user's home directory. Nil means [os.UserHomeDir]. It is consulted
	// only to resolve a default StateDir, and only when the layout's state directory falls back to
	// the home directory: never when <PREFIX>_HOME is set, on Linux when XDG_STATE_HOME is an
	// absolute path, or on Windows when LocalAppData is set.
	UserHomeDir func() (string, error)
}

// Validate returns nil when the Config can be used. Otherwise it returns one error listing every
// problem at once: each [validate.FieldError] of [spec.Identity.Validate], which wraps
// [validate.ErrEmpty] or [validate.ErrInvalid], and [ErrNoSource] when Source is nil, the one
// field a zero Config cannot default. Every other field is optional.
func (c Config) Validate() error {
	var noSource error
	if c.Source == nil {
		noSource = ErrNoSource
	}
	return validate.Join(c.Identity.Validate(), noSource)
}

// resolve validates c and returns a copy with every zero field set to its production default.
// It never modifies c. An empty StateDir is the state directory [paths.Resolve] resolves, for the
// [paths.State] kind alone, for the Identity with the resolved GOOS, Getenv and UserHomeDir, so a
// substituted seam decides it too; resolve fails when StateDir is empty and the state directory
// needs a home directory the resolver cannot find.
// An explicit StateDir is kept without consulting the resolver.
func (c Config) resolve() (Config, error) {
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	r := c
	if r.BinaryName == "" {
		r.BinaryName = r.Name
	}
	if r.TempPrefix == "" {
		r.TempPrefix = r.BinaryName + tempPrefixSuffix
	}
	if r.AssetTemplate == "" {
		r.AssetTemplate = defaultAssetTemplate
	}
	if r.ChecksumsAsset == "" {
		r.ChecksumsAsset = defaultChecksumsAsset
	}
	if r.HTTPClient == nil {
		r.HTTPClient = &http.Client{}
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.GOOS == "" {
		r.GOOS = runtime.GOOS
	}
	if r.GOARCH == "" {
		r.GOARCH = runtime.GOARCH
	}
	if r.Executable == nil {
		r.Executable = os.Executable
	}
	if r.Getenv == nil {
		r.Getenv = os.Getenv
	}
	if r.UserHomeDir == nil {
		r.UserHomeDir = os.UserHomeDir
	}
	if r.StateDir == "" {
		getenv := r.Getenv
		dirs, err := paths.Resolve(r.Identity, paths.Options{
			GOOS: r.GOOS,
			LookupEnv: func(key string) (string, bool) {
				v := getenv(key)
				return v, v != "" // [lydite:exclude_from_mutation][paths counts a variable set to "" as unset and reads only the value, so the flag is unobservable]
			},
			UserHomeDir: r.UserHomeDir,
		}, paths.State)
		if err != nil {
			return Config{}, fmt.Errorf("update: resolving StateDir: %w", err)
		}
		r.StateDir = dirs.State()
	}
	return r, nil
}
