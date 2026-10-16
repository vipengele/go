package spec

// ColorAllowed reports whether colour is on for stream s of env. It reads env only through
// LookupEnv and IsTerminal, and the first rule that applies decides:
//
//  1. noColor, the --no-color flag, turns colour off.
//  2. NO_COLOR or <EnvPrefix>_NO_COLOR set to a non-empty value turns colour off.
//  3. FORCE_COLOR set to a non-empty value other than "0" turns colour on, for a stream that is
//     not a terminal and over TERM=dumb alike.
//  4. TERM=dumb turns colour off.
//  5. Otherwise colour is on exactly when env.IsTerminal(s) reports a terminal.
//
// A variable set to "" counts as unset. IsTerminal is called only when rule 5 decides, so a
// short-circuited decision never queries the terminal. A nil identity, or one whose EnvPrefix
// names no variable, reads no prefixed variable; a nil env or a nil LookupEnv sets no variable.
func ColorAllowed(noColor bool, identity *Identity, env *Env, s Stream) bool {
	if noColor {
		return false
	}
	if envValue(env, "NO_COLOR") != "" {
		return false
	}
	if identity != nil {
		if name, err := identity.EnvVar("NO_COLOR"); err == nil && envValue(env, name) != "" {
			return false
		}
	}
	if force := envValue(env, "FORCE_COLOR"); force != "" && force != "0" {
		return true
	}
	if envValue(env, "TERM") == "dumb" {
		return false
	}
	return env.IsTerminal(s)
}

// envValue returns the value env sets for the variable name, and "" when it sets none.
func envValue(env *Env, name string) string {
	if env == nil || env.LookupEnv == nil {
		return ""
	}
	v, _ := env.LookupEnv(name)
	return v
}
