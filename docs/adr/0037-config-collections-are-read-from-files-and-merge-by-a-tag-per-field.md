# Config collections are read from files and merge by a tag per field

Status: accepted. It extends the schema of `go.vipengele.org/config`, which accepted single values
and slices of them, to maps and lists of structs.

A program such as gt keeps named profiles and templates in a list and per-name settings in a map,
and layers a user file with a project file. `go.vipengele.org/config` rejected maps and replaced
every value whole per key, so such a schema could not be described. A collection is now a key of the
schema, read from files, and each layer's collection is combined with the one beneath by the
strategy its field declares.

## The rules

- **Maps.** `map[string]T` is accepted where T is a scalar, a `time.Duration`, a scalar slice, a
  struct, or a nested map. A non-string key, a pointer, or a slice of maps is `ErrSchema`. Map keys
  are used verbatim: never normalised, never split on dots, and never passed through `matchKey`. A
  file accepts any key beneath a map, a null (`~`) or empty one included, verbatim, and is strict
  again inside struct values.
- **Slices of structs.** A slice of structs is accepted. The fields of an element read the `config`
  and `doc` tags.
- **The `merge` tag.** Without one, the later layer's whole collection replaces the earlier one.
  `merge:"<key>"` on a slice of structs matches elements on the element field whose config key is
  `<key>`, which must be a string: a matching element is replaced whole, where it stands, and keys
  the earlier layer lacks are appended in the later layer's order. `merge:"keys"` on a map merges one
  level deep: later wins per key and each value is replaced whole. The tag is allowed only on fields
  outside collection elements; any other use is `ErrSchema`. A duplicate or empty merge key within
  one file is an error at file:line; in the Go defaults it is a `Load` error naming "defaults". The
  defaults are the base layer of the merge.
- **Env and flags.** Collection fields, and every field inside them, have no Env or flag name:
  `Field.Env()` and `Field.Flag()` return `""` and the collision check skips `""`. `Env` skips them.
  `Flags` rejects a key that names one with "not settable from flags". Scanning the environment for
  collections stays rejected (ADR-0012).
- **Paths.** A path has one display form, `profiles[2].env["FOO"]`, with map keys in Go string
  quoting (`strconv.Quote`). One helper in `key.go` formats and parses it. In the merged result a
  path uses the merged index; a file problem uses the index within that file, beside its file:line.
- **Sources.** Each leaf has one `Source` with its own origin, and `Source` gains no field. An empty
  map or list, at any depth, is one row, `{}` or `[]`. A scalar slice is one row, its items joined by
  commas. Rows follow the schema's field order, then element order, with map entries sorted by key.
  An element field the file did not write takes its element's origin.
- **The Entry and Node contract.** A provider emits one `Entry` per collection field per layer; its
  `Node` holds the value as a tree. `Node` is exported and records its kind (scalar, list or map),
  its scalar value, its items, its map entries in written order, and an origin per node. Scalar
  fields keep `Value` and `Items`. Each Entry carries one whole layer of a collection, which gives
  merging its layer boundary; an empty element and a nested empty map are still recorded; map keys
  never reach `matchKey`; and the wire format holds no path. `Schema.Fields()` lists a collection as
  one `Field`, `Field.Elem()` returns the element sub-schema or nil, and `Schema.Lookup` takes
  schema keys, not paths. `annotate` resolves a `FieldError` path through the binder's path-to-origin
  index, which covers non-leaf nodes, so `profiles[2].env` gets the origin of the map and an empty
  `Value`.
- **Seed.** An empty struct collection is written as `[]` or `{}`, after the field's doc and a
  commented-out example element, written as `HeadComment` text, in which each element field appears
  with its doc; a map's example uses the placeholder key `<name>`. A non-empty default is written as
  real elements with docs on the first only. Read back through File, nil and empty count as equal.
- **Settled in the implementation.** After a keyed merge the collection as a whole takes the origin
  of the latest layer that set it, while kept entries keep theirs. A `FieldError` on an empty
  collection has Value `{}` or `[]`. A populated seeded element writes every field, so a nil value
  reads back as empty.

## Considered options

- **One Entry per leaf, keyed by a path.** A path key goes through `matchKey`, which lowercases and
  strips `-` and `_` across the whole string and corrupts a map key, and it carries no layer
  boundary, so merging could not tell an element a layer omitted from one it never had. A tree per
  collection keeps both.
- **Normalising map keys.** A map key is data, such as an environment variable name or a dotted host
  name, not a schema key. Relaxed matching would merge `FOO` and `foo` and split `a.b`.
- **Env and flag values for collections.** They need an encoding for nested structure that every
  user must learn and no environment scan can find (ADR-0012). Collections stay file-only.
- **Merging every collection by default.** Replacing whole is what a scalar slice has always done,
  and a schema that declares nothing keeps behaving as before; merging is opted into per field.

## Consequences

- **A schema of single values behaves as before.** Only a schema with a collection reaches the new
  paths.
- **A merged list can hold elements from several layers.** `Sources()` names the origin of each leaf
  so the merged value stays explainable.
- **Divergences from gt, recorded for its migration (vipengele/go#87).** gt's `Merge` silently drops
  an `auto_update` set in its project file, and an empty `worktree_types` there does not replace the
  user's list. Under these semantics, an `auto_update` in an overlay outside the allow-list of a
  file layer is an error, not dropped, and an empty list in the overlay replaces the list beneath
  it, where gt keeps the user's. gt's migration states both.
- **Per-element merging is one level.** A map value or a list element is replaced whole; there is no
  deep merge inside an element.
