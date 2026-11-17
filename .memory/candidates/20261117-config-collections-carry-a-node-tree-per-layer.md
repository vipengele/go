---
about: "config collections travel as one Entry with a Node tree per field per layer; map keys never pass through matchKey; merging and per-leaf origins depend on that boundary"
saw:
  - source/config/file.go
  - source/config/bind.go
  - source/config/key.go
  - source/config/schema.go
  - docs/adr/0037-config-collections-are-read-from-files-and-merge-by-a-tag-per-field.md
---

- A collection field (Field.Kind KindList or KindMap) arrives from a provider as one `Entry` whose
  `Node` holds the whole layer's value as a tree (file.go `Entry`, `Node`). One Entry per layer is
  the layer boundary `binder.bindCollection` merges on (bind.go); splitting a collection into one
  Entry per leaf loses it, and an empty element or map would vanish.
- `matchKey` and `Schema.byKey` lowercase and strip `-`/`_` across the whole string (key.go), so a
  map key must never reach them: File keeps map keys verbatim (`fileLoader.mapNode`), bind sets them
  through `reflect.Value.Convert` for named string key types (`build`), and element fields are found
  by exact canonical key (`elemField`).
- A struct element's Node is a NodeMap keyed by canonical element keys, dotted for nested structs
  ("meta.owner"); a map's Node is keyed verbatim. Only the Schema tells them apart (file.go `Node`
  doc).
- Paths have one display form, `profiles[2].env["FOO"]`, through `formatPath`/`parsePath` (key.go);
  the path→Source index built by `binder.sources` also holds non-leaf paths, which is how
  `annotate` (validate.go `resolveKey`) gives `profiles[2].env` an origin.
- Collections and their element fields have no Env or flag name: `Field.Env()`/`Flag()` are "" and
  `collide` skips "" (schema.go); Flags rejects such keys via `collectionAbove` (flags.go).
