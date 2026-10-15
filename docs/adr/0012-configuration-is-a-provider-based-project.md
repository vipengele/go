# Configuration is loaded by a provider-based project, `config`

Status: supersedes the part of ADR-0003 that assigns config loading to `cli`.

A tool's configuration comes from several places: defaults in code, a YAML file, environment
variables and command-line flags. `go.vipengele.org/config` loads them into one struct, layer over
layer, and reports for every key where its value came from.

```go
res, err := config.Load(defaults, config.File(path), config.Env("APP", os.LookupEnv), config.Flags(set))
```

`defaults` is the struct whose fields are the keys and whose values are the defaults. Each provider
is a layer above the previous one, and the conventional order is file, then environment, then
flags.

## A project of its own

`config` is a project of its own, `source/config`. ADR-0003 lists config loading among the things
`cli` owns, and ADR-0004 carries that into `cli/core`; a loader of this size, public and with its
own dependency, is a library a consumer may want without the app shell, so it is decided here.

## Keys

- **Relaxed matching.** A key is matched ignoring case, `-` and `_` within a segment, as Spring
  Boot's relaxed binding does: `max-retries`, `MAX_RETRIES` and `maxRetries` name `max_retries`.
  The canonical form is snake_case. Each provider spells a key in its own convention (a file in
  YAML style, a flag with dashes, a variable in upper case) and none of them needs a translation
  table.
- **Dotted keys equal nested YAML.** `.` separates levels, and `server.port: 80` sets the key that
  a `server` mapping holding `port: 80` sets. A file may use either, and flags and the schema use
  the dotted form.
- **Strict file and flags, lenient environment.** A key in a file or among the flags that is not in
  the schema is an error that names it, since a misspelled key silently ignored is a setting that
  does not apply. The environment is shared with other programs, so variables outside the schema
  are not the loader's to judge.
- **The environment is looked up per known field, never scanned.** The loader derives the variable
  of each key (`APP_SERVER_MAX_RETRIES`) and asks for that one. Scanning the environment for a
  prefix would have to turn `APP_A_B_C` back into a key, and that name is ambiguous between
  `a.b_c`, `a_b.c` and `a_b_c`. Deriving names from keys removes the question.
- **A missing, empty or comment-only file sets nothing.** The defaults stand.

## The loader has no CLI knowledge

The flags provider takes a map from key to text of the flags a command line set explicitly. A flag
left at its default is absent from the map, so it does not override a lower layer. The loader
parses no command line, declares no flag and imports no CLI framework. The adapter that turns a
`cli/spec` command's invocation into that map belongs to the project that knows both sides, and
another front end can supply the map from its own parser.

## Considered options

- **In `commons`.** `commons` holds generic building blocks any project may require, and every
  dependent would inherit the YAML library.
- **In `cli/core`.** ADR-0003's original home. A daemon or a library wanting configuration without
  the app shell, its command tree and its CLI framework would take all of it.
- **In `cli/spec`.** Its root package does no I/O, and a test enforces that. A file loader there
  would break the property that lets every project depend on the contracts cheaply, and the YAML
  dependency would reach every dependent.
- **Scanning the environment for a prefix.** Needs a rule to split `APP_A_B_C` into levels, which
  cannot be right for every schema.
- **Strict environment.** Rejects any `APP_`-prefixed variable outside the schema, including the
  ones other tools of the same family set.

## Consequences

- `config` requires no sibling. A tool requires it beside `cli/core` and joins them in its `main`.
- The YAML dependency is carried by `config` alone.
- Each key has one environment variable and one flag name, derived from it. The schema rejects
  fields whose keys, variables or flags collide.
