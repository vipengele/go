// Package envvar derives environment-variable names from a prefix and a suffix.
//
// A name is the prefix and the suffix joined by an underscore, and every part of it is a valid
// environment-variable identifier as defined by the validate package:
//
//	name, err := envvar.Name("MYAPP", "NO_COLOR") // "MYAPP_NO_COLOR"
package envvar
