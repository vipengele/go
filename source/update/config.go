package update

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/paths"
	"go.vipengele.org/commons/validate"
	"go.vipengele.org/update/asset"
)

// optOutSuffix is the suffix of the environment variable that turns update checks off. The
// variable's full name is Identity.EnvVar(optOutSuffix): "ATK_NO_UPDATE_CHECK" for EnvPrefix
// "ATK".
const optOutSuffix = "NO_UPDATE_CHECK"

// tempPrefixSuffix follows the binary name in the default [Config.TempPrefix].
const tempPrefixSuffix = "-update-"

const (
	// defaultMaxDownloadBytes is the [Config.MaxDownloadBytes] a zero value resolves to.
	defaultMaxDownloadBytes int64 = 256 << 20
	// defaultDownloadTimeout is the [Config.DownloadTimeout] a zero value resolves to.
	defaultDownloadTimeout = 5 * time.Minute
)

// Config is everything the update library needs to know about a tool, and every seam through
// which it reaches the outside world. Any field left at its zero value takes its production
// default, documented on the field. The embedded [spec.Identity] is required in full.
//
// Config holds func fields, so it is not comparable.
type Config struct {
	spec.Identity

	// BinaryName is the name of the tool's binary. Empty means Identity.Name.
	BinaryName string
	// TempPrefix prefixes the names of the temporary files an update writes. Empty means
	// BinaryName followed by "-update-".
	TempPrefix string
	// TempDir is the directory the temporary files of an update are created in. Empty means
	// [os.TempDir].
	TempDir string
	// MaxDownloadBytes is the most bytes a single download may read. Zero means 256 MiB; a negative
	// value is invalid.
	MaxDownloadBytes int64
	// DownloadTimeout bounds the whole of a single download. Zero means 5 minutes; a negative value
	// is invalid.
	DownloadTimeout time.Duration
	// AssetTemplate names the release asset that holds the binary for a platform, written in the
	// template language of package asset: {name}, {version}, {tag}, {os}, {arch} and {ext}. Empty
	// means [asset.DefaultTemplate].
	AssetTemplate string
	// AssetExt is the archive extension {ext} stands for, without a leading ".". Empty means
	// [asset.DefaultExt]. It carries no per-OS rules: a tool whose assets differ by platform
	// writes the difference into AssetTemplate, and a raw-binary tool leaves {ext} out of it.
	AssetExt string
	// ChecksumsAsset is the name of the release asset that lists every asset's checksum, written
	// in the template language of package asset: it takes every placeholder but {ext}. Empty means
	// [asset.DefaultChecksumsTemplate].
	ChecksumsAsset string
	// StateDir is the directory the library keeps its state in. Empty means the state directory
	// [paths.Resolve] resolves for the Identity, through the GOOS, Getenv and UserHomeDir seams: on
	// Linux $XDG_STATE_HOME/<DirName> or ~/.local/state/<DirName>, and <PREFIX>_HOME/state when
	// that variable is set. Only the state directory is resolved, so the home directory is needed
	// only when the state directory itself falls back to it. An explicit StateDir is used as given.
	StateDir string
	// Source reports the releases the tool can update to. Nil means the [GitHubSource] for the
	// Identity's Owner and Repo, making its requests through HTTPClient.
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
	// EvalSymlinks returns a path with every symbolic link in it resolved. Nil means
	// [filepath.EvalSymlinks]. It resolves the path Executable returns before the path is matched
	// against the detectors of externally managed installs.
	EvalSymlinks func(string) (string, error)
	// Detectors are the tool's own detectors of externally managed installs, consulted in order
	// after the built-in ones. Nil means the built-in detectors alone; a nil entry is invalid.
	Detectors []Detector
	// InContainer reports whether the process runs inside a container. Nil means
	// [paths.InContainer] for the resolved GOOS, reading the environment through Getenv and the
	// real filesystem.
	InContainer func() bool
}

// Validate returns nil when the Config can be used. Otherwise it returns one error listing every
// problem at once: each [validate.FieldError] of [spec.Identity.Validate], which wraps
// [validate.ErrEmpty] or [validate.ErrInvalid]. Every other field is optional, and a non-empty
// AssetTemplate, ChecksumsAsset or AssetExt must be valid: AssetTemplate by [asset.Validate],
// ChecksumsAsset by [asset.ValidateChecksums], and AssetExt holding none of "/", "\", "{" and
// "}" and not starting with ".". MaxDownloadBytes and DownloadTimeout must not be negative, and no
// entry of Detectors may be nil. Each of those is a [validate.FieldError] wrapping
// [validate.ErrInvalid], the one for a nil detector naming its index as Detectors[i], and the
// AssetTemplate and ChecksumsAsset ones also wrap the error of package asset.
func (c Config) Validate() error {
	return validate.Join(
		c.Identity.Validate(),
		checkTemplate("AssetTemplate", c.AssetTemplate, asset.Validate),
		checkTemplate("ChecksumsAsset", c.ChecksumsAsset, asset.ValidateChecksums),
		checkAssetExt(c.AssetExt),
		checkNotNegative("MaxDownloadBytes", c.MaxDownloadBytes),
		checkNotNegative("DownloadTimeout", int64(c.DownloadTimeout)),
		checkDetectors(c.Detectors),
	)
}

// checkDetectors rejects every nil entry of detectors; a nil slice means the built-ins alone.
func checkDetectors(detectors []Detector) error {
	var errs []error
	for i, d := range detectors {
		if d == nil {
			errs = append(errs, &validate.FieldError{
				Field: "Detectors[" + strconv.Itoa(i) + "]",
				Value: "nil",
				Err:   fmt.Errorf("%w: must not be nil", validate.ErrInvalid),
			})
		}
	}
	return validate.Join(errs...)
}

// lookupEnvOf adapts getenv to the LookupEnv of package paths, which counts a variable set to ""
// as unset.
func lookupEnvOf(getenv func(string) string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v := getenv(key)
		return v, v != "" // [lydite:exclude_from_mutation][paths counts a variable set to "" as unset and reads only the value, so the flag is unobservable]
	}
}

// checkNotNegative rejects a negative limit; zero means the default.
func checkNotNegative(field string, v int64) error {
	if v < 0 {
		return &validate.FieldError{Field: field, Value: strconv.FormatInt(v, 10), Err: fmt.Errorf("%w: must not be negative", validate.ErrInvalid)}
	}
	return nil
}

// checkTemplate applies check to a non-empty template; an empty one means the default.
func checkTemplate(field, tmpl string, check func(string) error) error {
	if tmpl == "" {
		return nil
	}
	if err := check(tmpl); err != nil {
		return &validate.FieldError{Field: field, Value: tmpl, Err: fmt.Errorf("%w: %w", validate.ErrInvalid, err)}
	}
	return nil
}

// checkAssetExt rejects an extension that would change the asset name's structure: a path
// separator, a brace that could open a placeholder, or a leading "." the template already writes.
func checkAssetExt(ext string) error {
	if ext == "" {
		return nil
	}
	if strings.ContainsAny(ext, `/\{}`) || strings.HasPrefix(ext, ".") {
		return &validate.FieldError{
			Field: "AssetExt",
			Value: ext,
			Err:   fmt.Errorf("%w: must not contain '/', '\\', '{' or '}' or start with '.'", validate.ErrInvalid),
		}
	}
	return nil
}

// resolve validates c and returns a copy with every zero field set to its production default.
// It never modifies c. An empty StateDir is the state directory [paths.Resolve] resolves, for the
// [paths.State] kind alone, for the Identity with the resolved GOOS, Getenv and UserHomeDir, so a
// substituted seam decides it too; resolve fails when StateDir is empty and the state directory
// needs a home directory the resolver cannot find.
// An explicit StateDir is kept without consulting the resolver. A nil InContainer is likewise
// built over the resolved GOOS and Getenv.
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
	if r.TempDir == "" {
		r.TempDir = os.TempDir()
	}
	if r.MaxDownloadBytes == 0 {
		r.MaxDownloadBytes = defaultMaxDownloadBytes
	}
	if r.DownloadTimeout == 0 {
		r.DownloadTimeout = defaultDownloadTimeout
	}
	if r.AssetTemplate == "" {
		r.AssetTemplate = asset.DefaultTemplate
	}
	if r.AssetExt == "" {
		r.AssetExt = asset.DefaultExt
	}
	if r.ChecksumsAsset == "" {
		r.ChecksumsAsset = asset.DefaultChecksumsTemplate
	}
	if r.HTTPClient == nil {
		r.HTTPClient = &http.Client{}
	}
	if r.Source == nil {
		r.Source = NewGitHubSource(r.Identity, r.HTTPClient)
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
		dirs, err := paths.Resolve(r.Identity, paths.Options{
			GOOS:        r.GOOS,
			LookupEnv:   lookupEnvOf(r.Getenv),
			UserHomeDir: r.UserHomeDir,
		}, paths.State)
		if err != nil {
			return Config{}, fmt.Errorf("update: resolving StateDir: %w", err)
		}
		r.StateDir = dirs.State()
	}
	if r.EvalSymlinks == nil {
		r.EvalSymlinks = filepath.EvalSymlinks
	}
	if r.InContainer == nil {
		probes := paths.Probes{GOOS: r.GOOS, LookupEnv: lookupEnvOf(r.Getenv)}
		r.InContainer = func() bool { return paths.InContainer(probes) }
	}
	return r, nil
}
