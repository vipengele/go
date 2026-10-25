---
about: "logtest.Record.Attrs flattens groups to dot-joined keys in one map built in write order, so a duplicate key and a group key colliding with a literal dotted key ('http.status') both resolve to the last value written; testing/slogtest cannot be used because Record has no time field"
saw:
  - source/cli/core/logging/logtest/recorder.go
  - source/cli/core/logging/logtest/recorder_test.go
---

Verified in `source/cli/core/logging/logtest/recorder.go`:

- `Handle` builds the attribute list as the handler's own `WithAttrs` attributes (already
  qualified by the groups open when they were added) followed by the record's attributes qualified
  by the handler's group prefix, then fills `Record.Attrs` in that order. Because it is a plain
  map, the last write wins for a repeated key and for `slog.Group("http", slog.Int("status", 200))`
  beside a literal `"http.status"` key; the `Record` doc comment states this and
  `recorder_test.go` covers both orders.
- Values are `Value.Resolve()`d and stored as `Value.Any()`, so `slog.Int` is an `int64` and a
  `LogValuer` that resolves to a group is flattened. The assertion helpers in `assert.go` compare
  with `reflect.DeepEqual` on that stored value, so a wanted plain `int` does not match.
- `testing/slogtest` is not applicable: it requires a time key in each result and `Record` has no
  time field.
