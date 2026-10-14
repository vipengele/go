// Package buildinfo reports the version, commit and build date of the running binary, and the
// Go toolchain, operating system and architecture it was built for. It depends on the standard
// library only.
//
// # Resolution
//
// [Read] resolves each field on its own. A value set at link time with -X wins; otherwise the
// version is the main module's version from [runtime/debug.ReadBuildInfo], the commit its
// vcs.revision setting and the date its vcs.time setting. A main module version of "(devel)"
// counts as unknown. The Go version, operating system and architecture come from the [runtime]
// package. A value nothing supplies is "".
//
// # Link-time values
//
// A release build sets the version, the commit and the date with the linker:
//
//	go build -ldflags "\
//	  -X go.vipengele.org/cli/core/buildinfo.version=v1.0.0 \
//	  -X go.vipengele.org/cli/core/buildinfo.commit=0123abcdef0123abcdef0123abcdef0123abcdef \
//	  -X go.vipengele.org/cli/core/buildinfo.date=2026-01-02T03:04:05Z"
//
// The linker sets a -X variable only when it is a string variable with no initialiser or a
// constant one, and only when the program references it. It ignores any other -X silently, with
// no error and no warning, so the three variables stay uninitialised and [Read] references them.
//
// # Text
//
// [Info.String] is one line, and that line is the whole text contract: the version, then
// " build " and the commit shortened to 12 characters when a commit is known.
//
//	v1.0.0 build 0123abcdef01
//	v1.0.0
//	dev
//
// The version is printed as given, a pseudo-version included, and as "dev" when it is unknown.
//
// # JSON
//
// [Info.MarshalJSON] encodes an Info as one object, every key always present:
//
//	{"schema":1,"version":"v1.0.0","commit":"0123abcdef0123abcdef0123abcdef0123abcdef",
//	 "date":"2026-01-02T03:04:05Z","go":"go1.27.2","os":"linux","arch":"amd64","dev":false}
//
// commit is the full SHA, date is passed through as given, unvalidated, and dev is
// [Info.IsDev]. Every string value is "" when it is unknown. schema is 1. Adding a key is a
// compatible change and leaves schema as it is; renaming, removing or retyping a key increments
// schema.
package buildinfo
