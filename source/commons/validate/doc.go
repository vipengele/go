// Package validate checks single string fields against fixed syntactic rules and reports every
// failing field together.
//
// Each check takes the name of the field and its value, and returns nil when the value is
// accepted or a [*FieldError] naming the field, quoting the value and wrapping [ErrEmpty] or
// [ErrInvalid]. A check never alters its input: surrounding whitespace, a different case or a
// stray separator is rejected, not trimmed or folded. Every rule admits only printable ASCII,
// so no accepted value contains a path separator, a NUL byte or whitespace.
//
// [Join] combines the results of several checks into one error, nil when all of them passed:
//
//	func (c Config) Validate() error {
//		return validate.Join(
//			validate.Slug("name", c.Name),
//			validate.GitHubOwner("owner", c.Owner),
//			validate.RepoName("repo", c.Repo),
//			validate.EnvVar("prefix", c.Prefix),
//		)
//	}
//
// The joined error mentions every failing field, and [errors.Is] reports [ErrEmpty] or
// [ErrInvalid] through it.
package validate
