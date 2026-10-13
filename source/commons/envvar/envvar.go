package envvar

import "go.vipengele.org/commons/validate"

// Name returns prefix + "_" + suffix. Both parts and the joined name must satisfy
// [validate.EnvVar]; the parts are checked together, so the error lists every failing field, and
// the joined name is checked as "name" because it can exceed [validate.MaxEnvVarLen] when each
// part is within it. On failure the name is empty and the error wraps [validate.ErrEmpty] or
// [validate.ErrInvalid]. The parts are used exactly as given: nothing is upper-cased or trimmed.
func Name(prefix, suffix string) (string, error) {
	if err := validate.Join(
		validate.EnvVar("prefix", prefix),
		validate.EnvVar("suffix", suffix),
	); err != nil {
		return "", err
	}
	name := prefix + "_" + suffix
	if err := validate.EnvVar("name", name); err != nil {
		return "", err
	}
	return name, nil
}
