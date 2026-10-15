---
about: "the config YAML file provider must detect duplicate keys itself because go.yaml.in/yaml/v3 decoding into a yaml.Node accepts them; a key set twice under any spelling is an error, and an explicit null is refused"
saw:
  - source/config/file.go
  - source/config/file_test.go
  - source/config/testdata/
---

- `yaml.Node` decoding does not reject a repeated mapping key (`source/config/file.go`), so `fileLoader.pair` tracks `seen` by `matchKey` and reports the later spelling and the first one's line. Dotted `a.b` and nested `a: {b:}` are the same key.
- Only value keys are checked for duplicates; a group mapping may repeat and its keys merge, which lets `server.port:` and `server:` sit side by side.
- A missing, empty, comment-only or null-document file returns no entries and no error; an explicit `key:` null, or a null list element, is an error naming the key. Origins use the file's base name and the value's line (`config.yaml:12`).
- A second YAML document is an error; aliases are followed, and a merge key `<<` surfaces as `unknown key "<<"`.
