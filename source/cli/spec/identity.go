package spec

import (
	"fmt"

	"go.vipengele.org/commons/envvar"
	"go.vipengele.org/commons/validate"
)

// MaxEnvPrefixLen is the longest [Identity.EnvPrefix] that [Identity.Validate] accepts, in bytes.
// It leaves room within [validate.MaxEnvVarLen] for the suffix [Identity.EnvVar] appends.
const MaxEnvPrefixLen = 32

// Identity is the identity of a command-line tool: every value that is fixed per tool and that
// the library threads through. It is declared once, in the tool's main. Every field is required
// and stands on its own: none is derived from another, and none is normalized. The zero value is
// not valid; [Identity.Validate] reports what is missing.
type Identity struct {
	// Name is the binary's name: a lowercase slug of at most [validate.MaxSlugLen] bytes.
	Name string
	// Owner is the GitHub user or organization owning the tool's repository, at most
	// [validate.MaxGitHubOwnerLen] bytes.
	Owner string
	// Repo is the name of the tool's GitHub repository, at most [validate.MaxRepoNameLen] bytes.
	Repo string
	// EnvPrefix is the prefix of the tool's environment variables: an environment-variable
	// identifier of at most [MaxEnvPrefixLen] bytes, without the joining underscore.
	EnvPrefix string
	// DirName is the single path segment the tool owns under the platform's configuration,
	// state and cache roots, at most [validate.MaxPathSegmentLen] bytes and never starting
	// with '.'.
	DirName string
}

// Validate checks every field and returns nil when all of them pass. Otherwise it returns one
// error listing a [validate.FieldError] for each failing field, named as the Go field, so a
// caller sees every problem at once. Each wraps [validate.ErrEmpty] for an empty field and
// [validate.ErrInvalid] for any other rejection. The fields are checked as given, never
// trimmed or case-folded.
func (i Identity) Validate() error {
	return validate.Join(
		validate.Slug("Name", i.Name),
		validate.GitHubOwner("Owner", i.Owner),
		validate.RepoName("Repo", i.Repo),
		checkEnvPrefix(i.EnvPrefix),
		checkDirName(i.DirName),
	)
}

// EnvVar returns the name of the tool's environment variable with the given suffix,
// EnvPrefix + "_" + suffix, as [envvar.Name] builds it: EnvVar("NO_COLOR") with EnvPrefix "ATK"
// is "ATK_NO_COLOR". On failure it returns "" and an error wrapping [validate.ErrEmpty] or
// [validate.ErrInvalid]. It never reads the environment.
func (i Identity) EnvVar(suffix string) (string, error) {
	return envvar.Name(i.EnvPrefix, suffix)
}

// Slug returns the tool's repository as "Owner/Repo". It does not validate either part.
func (i Identity) Slug() string {
	return i.Owner + "/" + i.Repo
}

// Subdir returns DirName + "/" + name, a slash-separated path relative to a platform root, for
// a name that is a single path segment as [validate.PathSegment] accepts it. An invalid DirName
// is never hidden behind a valid name: on failure Subdir returns "" and an error listing the
// DirName rejection and the name rejection, whichever apply. It resolves no platform root and
// touches no file system.
func (i Identity) Subdir(name string) (string, error) {
	if err := validate.Join(
		checkDirName(i.DirName),
		validate.PathSegment("name", name),
	); err != nil {
		return "", err
	}
	return i.DirName + "/" + name, nil
}

// checkEnvPrefix accepts an environment-variable identifier of at most MaxEnvPrefixLen bytes.
func checkEnvPrefix(value string) error {
	if err := validate.EnvVar("EnvPrefix", value); err != nil {
		return err
	}
	if len(value) > MaxEnvPrefixLen {
		return &validate.FieldError{
			Field: "EnvPrefix",
			Value: value,
			Err:   fmt.Errorf("%w: must be at most %d bytes", validate.ErrInvalid, MaxEnvPrefixLen),
		}
	}
	return nil
}

// checkDirName accepts a single path segment. validate.PathSegment requires a lowercase letter
// or a digit first, so a DirName that would hide its directory behind a leading '.' is rejected.
func checkDirName(value string) error {
	return validate.PathSegment("DirName", value)
}
