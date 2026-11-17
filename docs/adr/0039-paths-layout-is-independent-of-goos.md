# Paths layout is chosen independently of GOOS

Status: amends ADR-0009.

`cli/spec/paths` resolves the directories of a tool under one of three layouts, and ADR-0009 had
`Options.GOOS` pick it. A tool that wants the XDG layout on macOS, as many command-line tools do,
or a test that wants one layout on any host, has no way to say so without changing the operating
system the resolver believes it runs on. The layout is now a choice of its own.

## The decision

- **`paths.Layout`** is `LayoutDefault`, `LayoutXDG`, `LayoutDarwin` or `LayoutWindows`, with
  `String` and `Valid`. The zero value, `LayoutDefault`, takes the layout from `Options.GOOS`
  exactly as ADR-0009 describes, so a caller that sets nothing resolves nothing differently.
- **`Options.Layout`** selects the layout. A chosen `LayoutXDG` honours the `XDG_*_HOME`
  variables on every operating system, macOS and Windows included.
- **Precedence.** The kind's explicit option, `Options.Home`, `<PREFIX>_HOME`, then the layout's
  own rules, as before. The layout decides only the last step.
- **An unknown `Layout` is an error,** and `Resolve` reports it before the `Options.Home` and
  `<PREFIX>_HOME` shortcut. A home root resolves no layout, so checking afterwards would let an
  invalid value pass wherever a home root is set and fail only elsewhere.
- **`GOOS` stays the target operating system** for everything that is not a directory layout.
  `Ensure` skips the mode change when `GOOS` is Windows, because mode bits do not describe access
  there, whichever layout the directories were resolved under.
- **`cli/core`** takes the layout as an option, `WithPathsLayout`, which applies to every directory
  the `App` resolves, and a hook reads it back with `PathsLayoutFrom(ctx)`. An unknown layout makes
  the command tree invalid. It is a context accessor and not a method of `spec.Invocation`:
  `paths` imports `spec`, so `spec` naming `paths.Layout` would be an import cycle.
- **`update`** has the field `Config.PathsLayout`, forwarded to `paths` when `StateDir` is empty. An
  unknown layout is a validation error of the `Config`.

## Considered options

- **Setting `GOOS` to `linux` to get the XDG layout.** In `update`, `GOOS` also picks the release
  asset, the `.exe` name and container detection, so a tool on macOS would download the Linux
  archive. A layout is a property of where files go, not of which binary runs.
- **A public `GOOS` option in `cli/core`.** It would hand every tool the same hazard, a
  half-faked operating system, and `cli/core` has no use for the target operating system beyond
  directories. The seam stays in `paths` and in `update`'s `Config`, where tests already inject it.
- **A method on `spec.Invocation`.** An import cycle, as above.

## What this changes in earlier ADRs

- **ADR-0009.** "A test selects any layout on any host by setting `Options.GOOS`" is one way among
  two: `Options.Layout` selects it without changing `GOOS`. The list of layouts and the order of
  the per-kind rules are unchanged.

## Consequences

- A tool that sets no layout behaves as before on every operating system.
- A tool can keep one layout on every system it ships for, and its tests cover each layout on a
  single host without touching `GOOS`.
- A Windows layout resolved on another host still creates directories with the mode `Ensure` uses
  there, and one resolved on Windows under another layout still skips the mode change.
