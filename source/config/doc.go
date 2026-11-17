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
// takes the providers in the order given and the last one to set a key wins; a slice of
// single values is replaced whole, and a collection (see Collections) is replaced or merged as its
// `merge` tag says. The package reads no command line and never reads the process environment:
// the caller passes os.LookupEnv, or a fake, and the map of the flags that were set explicitly.
//
// # Keys
//
// A key is a dotted path with one segment per level of struct nesting, such as
// "server.max_retries". The canonical form of a segment is snake_case. A segment is the name in the
// field's `config:"name"` tag or, without one, the field name; "maxRetries" and MaxRetries both
// give "max_retries". A field tagged `config:"-"` and an unexported field have no key. A key holds
// a bool, a signed or unsigned integer, a float, a string, a [time.Duration], or a slice of one of
// these, or a collection; a pointer, an embedded field, a map with a key that is not a string or any
// other type is a schema error, reported by Load and by [NewSchema] as matching [ErrSchema].
//
// A key is matched relaxed: case, "-" and "_" within a segment are ignored, so "max-retries",
// "MAX_RETRIES" and "maxRetries" all name "max_retries". A "." always separates levels. In a file,
// "server.port: 80" and a "server" mapping holding "port: 80" set the same key.
//
// # Collections
//
// A key may also hold a collection: a slice of structs, or a map with string keys whose values are
// single values, slices of them, structs or further such maps. A collection is read from files only:
// it and every field of its element have no environment variable and no flag, [Env] skips them and
// [Flags] rejects a key naming one. The fields of an element are keyed as those of the configuration
// struct are, `config` and `doc` tags included, and are as strict as the top level: an unknown
// field is an error. A map key is the exception. It is used verbatim, as written, never normalised
// and never split on ".", and any key, null or empty included, is accepted beneath a map.
//
// A `merge:"..."` tag says how the layers of a collection combine. Without one, the collection of
// the last layer that sets it replaces the earlier ones whole. On a slice of structs,
// `merge:"name"` matches elements on the string field whose key is "name": a matching element
// replaces the earlier one where it stands, and an element with no match is appended in the order
// of its layer. On a map, `merge:"keys"` merges one level deep: the later layer wins per key and
// each value is replaced whole. The tag is allowed only on a field outside a collection element.
// Within one file a duplicate or empty merge key is an error at its line, and in the defaults a
// [Load] error naming "defaults". The defaults are the base layer of the merge.
//
// A path into a collection is written profiles[2].env["FOO"]: a list index in brackets, a map key
// in Go string quoting. In the merged result the index is the position in the merged list; a
// problem within a file names the position within that file, beside its line.
//
// In the Sources of a [Result] each leaf of a collection has a row of its own, in the order of the
// schema's fields, then element order, with map entries sorted by key. An empty map or list is one
// row, "{}" or "[]". A scalar slice is one row, its items joined by commas. A field of an element
// that its layer did not write takes the origin of the element. After a keyed merge the origin of
// each kept entry stays that of its layer, and the collection as a whole takes the origin of the
// latest layer that set it. A [*FieldError] on an empty collection carries the value "{}" or "[]".
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
// [FileProvider.Allow] limits the keys a file may set. A key outside the list, or outside a group
// it names, is an error at its line, "key ... is not settable in <file>", never dropped; a key that
// names a group or a collection allows all of it, and keys inside collection elements and map keys
// are never checked. Keys match as schema keys do. Allow with no keys, or no call, allows every key.
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
// create on first run. A collection that is empty is written as [] or {}, after its doc and a
// commented example element; one with defaults is written as elements, documented on the first. It
// is the one write the package makes: it creates the file with mode 0600, never replaces an
// existing one and creates no directory. A tool that wants no default file does not call it.
package config
