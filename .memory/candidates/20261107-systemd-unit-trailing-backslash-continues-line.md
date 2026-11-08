---
about: "systemd joins a unit-file line ending in an unescaped backslash with the next one, so renderUnit never ends a raw WorkingDirectory= or Description= line in a backslash"
saw:
  - source/daemon/service/systemd_unit.go
  - source/daemon/service/systemd_unit_test.go
---

`renderUnit` (source/daemon/service/systemd_unit.go) writes `WorkingDirectory=` and `Description=`
raw, with only `%` doubled, because systemd applies no C-style unescaping to either. systemd's unit
parser treats a line ending in an odd run of backslashes as continued onto the next line, so a raw
value ending in `\` would swallow the directive after it. For a system unit, that directive can be
`User=`, and the service would then run as root.

- WorkingDirectory: a value ending in `\` gets a `/` appended, which names the same directory.
- Description: a trailing run of `\` is dropped; a Description of only backslashes writes no line.
- Quoted words (`ExecStart=`, `Environment=`, `ReadWritePaths=`) are safe: `quoteWord` doubles every
  `\` inside the quotes.
- `checkNoContinuation` in systemd_unit_test.go fails any golden line ending in an odd backslash run.
- Newlines cannot reach a raw value: `Config.validate` (config.go) rejects control characters.
