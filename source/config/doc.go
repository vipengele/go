// Package config loads the configuration of a program from layered providers into a struct.
//
// The struct describes the configuration: each exported field is a key, and the value the struct
// holds when it is passed to [Load] is the default of every key. Providers are layers above the
// defaults, each overriding the keys it sets:
//
//	res, err := config.Load(defaults,
//		config.File(path),
//		config.Env("APP", os.LookupEnv),
//		config.Flags(set),
//	)
//
// The conventional order is defaults, then [File], then [Env], then [Flags], so that a flag
// overrides an environment variable, which overrides a file, which overrides the defaults. Load
// takes the providers in the order given and the last one to set a key wins; a slice is replaced
// whole, never merged. The package reads no command line and never reads the process environment:
// the caller passes os.LookupEnv, or a fake, and the map of the flags that were set explicitly.
//
// # Keys
//
// A key is a dotted path with one segment per level of struct nesting, such as
// "server.max_retries". The canonical form of a segment is snake_case. A segment is the name in the
// field's `config:"name"` tag or, without one, the field name; "maxRetries" and MaxRetries both
// give "max_retries". A field tagged `config:"-"` and an unexported field have no key. A key holds
// a bool, a signed or unsigned integer, a float, a string, a [time.Duration], or a slice of one of
// these; a pointer, an embedded field, a map or any other type is a schema error, reported by Load
// and by [NewSchema] as matching [ErrSchema].
//
// A key is matched relaxed: case, "-" and "_" within a segment are ignored, so "max-retries",
// "MAX_RETRIES" and "maxRetries" all name "max_retries". A "." always separates levels. In a file,
// "server.port: 80" and a "server" mapping holding "port: 80" set the same key.
//
// # Strictness
//
// [File] and [Flags] are strict: a key that is not in the schema is an error that names it, so a
// misspelled key is never silently ignored. [Env] is lenient. It looks up the one variable derived
// from each key of the schema, such as APP_SERVER_MAX_RETRIES, and never scans the environment, so
// other variables are ignored. Deriving names from keys, rather than keys from names, leaves no
// ambiguity over where a name such as APP_A_B_C splits into levels.
//
// A file that is missing, empty or holds only comments sets nothing, and its keys keep the values
// of the layers below. A file that is not valid YAML, or has more than one document, is an error.
//
// # Sources
//
// A key is converted to the type of its field from the text a provider found. Every problem is
// reported in one error that names the key, its text and its origin. On success the [Result]
// holds the bound value and, for each key, a [Source]: the effective value and its origin, such as
// "config.yaml:12", "env APP_SERVER_MAX_RETRIES", "flag --server.max-retries", or "default" for a
// key no provider set.
//
// Any type with the methods of [Provider] is a layer of its own.
//
// # Documentation
//
// A `doc:"..."` tag describes a key; on a field of struct type it describes the group of keys below
// it. [Field.Doc] and [Schema.GroupDoc] return the text, and [Seed] writes it as a comment above the
// key. A `doc` tag on a field that has no key is a schema error.
//
// # Validation
//
// A configuration type may implement [Validator]. [Load] calls Validate on a pointer to the bound
// value once every provider has loaded and every value has converted, so a value receiver and a
// pointer receiver both work. Only the top-level type is asked, never a nested struct. Validate
// must not change the value: Load returns it as Validate leaves it, with the Sources recorded
// before the call. A problem with one key is a [*FieldError] naming the key, alone or within the
// error Validate returns; Load reports it with the value and origin of the key.
//
// # Removed keys
//
// A configuration type may have a method RemovedKeys() map[string]string, mapping each removed key
// to a hint telling the user what to write instead. A removed key in a file, among
// the flags, or in a [Provider] that asks [Schema.Removed], is reported with its hint rather than
// as an unknown key. The method is found on the type or a pointer to it, and only on the top-level
// type. A removed key that is a live key, the group of one, or another removed key is a schema
// error.
//
// # Default file
//
// [Seed] writes a commented configuration file holding every key at its default, for a tool to
// create on first run. It is the one write the package makes: it creates the file with mode 0600,
// never replaces an existing one and creates no directory. A tool that wants no default file does
// not call it.
package config
