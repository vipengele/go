package validate

import (
	"errors"
	"fmt"
	"strings"
)

// Length bounds, in bytes. Every accepted value is ASCII, so a byte is a character.
const (
	// MaxSlugLen is the longest value [Slug] accepts.
	MaxSlugLen = 64
	// MaxGitHubOwnerLen is the longest value [GitHubOwner] accepts, GitHub's own limit for a
	// user or organization name.
	MaxGitHubOwnerLen = 39
	// MaxRepoNameLen is the longest value [RepoName] accepts, GitHub's own limit for a
	// repository name.
	MaxRepoNameLen = 100
	// MaxEnvVarLen is the longest value [EnvVar] accepts.
	MaxEnvVarLen = 64
	// MaxPathSegmentLen is the longest value [PathSegment] accepts.
	MaxPathSegmentLen = 64
)

// ErrEmpty is wrapped by every [FieldError] whose value is the empty string.
var ErrEmpty = errors.New("value is empty")

// ErrInvalid is wrapped by every [FieldError] whose value is non-empty and breaks the rule of
// the check that rejected it.
var ErrInvalid = errors.New("value is invalid")

// FieldError reports a value a check rejected. Err wraps [ErrEmpty] or [ErrInvalid].
type FieldError struct {
	// Field is the name of the field the caller passed to the check.
	Field string
	// Value is the rejected value, exactly as passed.
	Value string
	// Err is the cause; errors.Is matches it against ErrEmpty and ErrInvalid.
	Err error
}

// Error names the field and shows the rejected value quoted, followed by the cause.
func (e *FieldError) Error() string {
	return fmt.Sprintf("%s %q: %v", e.Field, e.Value, e.Err)
}

// Unwrap returns the cause, so errors.Is and errors.As see through a FieldError.
func (e *FieldError) Unwrap() error {
	return e.Err
}

// Join combines the results of several checks into one error. Nil results are discarded, and
// Join returns nil when every check passed. The joined error lists each failing field on its own
// line, and errors.Is and errors.As see every FieldError in it.
func Join(errs ...error) error {
	return errors.Join(errs...)
}

// Slug accepts a lowercase slug: a lowercase ASCII letter followed by lowercase letters, digits
// and hyphens, at most [MaxSlugLen] bytes, not ending in a hyphen.
func Slug(field, value string) error {
	if value == "" {
		return empty(field)
	}
	if len(value) > MaxSlugLen {
		return tooLong(field, value, MaxSlugLen)
	}
	if !isLower(value[0]) {
		return invalid(field, value, "must start with a lowercase letter")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isLower(c) && !isDigit(c) && c != '-' {
			return invalid(field, value, "must contain only a-z, 0-9 and '-'")
		}
	}
	if value[len(value)-1] == '-' {
		return invalid(field, value, "must not end with '-'")
	}
	return nil
}

// GitHubOwner accepts a GitHub user or organization name: ASCII letters of either case, digits
// and hyphens, at most [MaxGitHubOwnerLen] bytes, neither starting nor ending with a hyphen.
func GitHubOwner(field, value string) error {
	if value == "" {
		return empty(field)
	}
	if len(value) > MaxGitHubOwnerLen {
		return tooLong(field, value, MaxGitHubOwnerLen)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isLower(c) && !isUpper(c) && !isDigit(c) && c != '-' {
			return invalid(field, value, "must contain only A-Z, a-z, 0-9 and '-'")
		}
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return invalid(field, value, "must not start or end with '-'")
	}
	return nil
}

// RepoName accepts a GitHub repository name: ASCII letters of either case, digits, '.', '_' and
// '-', at most [MaxRepoNameLen] bytes, neither "." nor "..", and not ending in ".git".
func RepoName(field, value string) error {
	if value == "" {
		return empty(field)
	}
	if len(value) > MaxRepoNameLen {
		return tooLong(field, value, MaxRepoNameLen)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isLower(c) && !isUpper(c) && !isDigit(c) && c != '.' && c != '_' && c != '-' {
			return invalid(field, value, "must contain only A-Z, a-z, 0-9, '.', '_' and '-'")
		}
	}
	if value == "." || value == ".." {
		return invalid(field, value, "must not be '.' or '..'")
	}
	if strings.HasSuffix(value, ".git") {
		return invalid(field, value, "must not end with '.git'")
	}
	return nil
}

// EnvVar accepts an environment-variable identifier: an uppercase ASCII letter followed by
// uppercase letters, digits and underscores, at most [MaxEnvVarLen] bytes, with no "__" and not
// ending in an underscore.
func EnvVar(field, value string) error {
	if value == "" {
		return empty(field)
	}
	if len(value) > MaxEnvVarLen {
		return tooLong(field, value, MaxEnvVarLen)
	}
	if !isUpper(value[0]) {
		return invalid(field, value, "must start with an uppercase letter")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isUpper(c) && !isDigit(c) && c != '_' {
			return invalid(field, value, "must contain only A-Z, 0-9 and '_'")
		}
		if c == '_' && i > 0 && value[i-1] == '_' {
			return invalid(field, value, "must not contain '__'")
		}
	}
	if value[len(value)-1] == '_' {
		return invalid(field, value, "must not end with '_'")
	}
	return nil
}

// PathSegment accepts one segment of a path: a lowercase ASCII letter or digit followed by
// lowercase letters, digits, '.', '_' and '-', at most [MaxPathSegmentLen] bytes. It holds no
// separator, so it names exactly one directory entry. Its first byte is never '.', so "." and
// ".." are rejected.
func PathSegment(field, value string) error {
	if value == "" {
		return empty(field)
	}
	if len(value) > MaxPathSegmentLen {
		return tooLong(field, value, MaxPathSegmentLen)
	}
	if value == "." || value == ".." {
		return invalid(field, value, "must not be '.' or '..'")
	}
	if !isLower(value[0]) && !isDigit(value[0]) {
		return invalid(field, value, "must start with a lowercase letter or a digit")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isLower(c) && !isDigit(c) && c != '.' && c != '_' && c != '-' {
			return invalid(field, value, "must contain only a-z, 0-9, '.', '_' and '-'")
		}
	}
	return nil
}

// The byte classes below are the only bytes any check accepts, together with '-', '.' and '_'.
// None of them is a separator ('/', '\'), NUL, whitespace or a non-ASCII byte.

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func empty(field string) error {
	return &FieldError{Field: field, Err: ErrEmpty}
}

func tooLong(field, value string, maxLen int) error {
	return invalid(field, value, fmt.Sprintf("must be at most %d bytes", maxLen))
}

func invalid(field, value, reason string) error {
	return &FieldError{Field: field, Value: value, Err: fmt.Errorf("%w: %s", ErrInvalid, reason)}
}
