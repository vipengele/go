# JSON output is a flat object led by `schema`, written by `spec.WriteJSON`

Status: amends ADR-0007 and ADR-0009.

A command that writes JSON is read by a program, so the shape it writes is a contract that has to
change visibly. Every JSON **Data** a command writes (ADR-0013) is one **Envelope**: a JSON object
whose first key is `schema`, an integer, followed by the command's own keys in the same flat object.
The `version --json` contract of ADR-0007 is the first of them.

## The envelope

- **Flat.** `{"schema":1,"version":"v1.0.0",...}`: the command's keys sit beside `schema`, not under
  a second key. Adding a key keeps `schema`; renaming, removing or retyping a key increments it
  (ADR-0007). A reader that ignores unknown keys survives every compatible change.
- **`schema`, not `version`.** `version` is already a key of `version --json`'s payload, and a
  second meaning for the same word in the same object is a reader's trap. The version of the shape
  is `schema`.
- **Written by a function.** `spec.WriteJSON(w, schema, v)` marshals `v`, which must be a JSON object
  without a `schema` key, and writes it indented by two spaces, `schema` first, with a trailing
  newline. It returns an error, and writes nothing, for a value that is not an object, a `schema`
  below 1, or a value that already carries a `schema` key. A command passes `inv.Env.Stdout` as `w`
  (ADR-0013). It is a free function: it needs nothing from the **Invocation** but its writer.
- **Indented.** `version --json` is indented like every other envelope. A consumer decodes the
  object; whitespace is not part of the contract. `update`'s preflight, as ADR-0007 specifies it, decodes the object.
  `buildinfo.Info` marshals its keys without `schema`; the envelope adds it.

## One format, two flags

`spec.Output` carries a `spec.Format`, `FormatText` or `FormatJSON`, with text as the zero value. A
command reads `inv.Output.Format()` and never a flag handle: `Command.Validate` rejects a descendant
that redeclares an inherited flag (ADR-0009), so a command cannot hold its own handle to a flag the
App owns.

An App takes the global `--output text|json` (`WithOutput`), the global `--json` (`WithJSON`), or
neither; taking both panics at construction. The two are conflicting spellings of one decision, and
these are usage errors, exit 2:

- `--output text` with `--json` true;
- `--output json` with an explicit `--json=false`;
- an `--output` value other than `text` or `json`.

Detecting a conflict needs to know whether a flag was set or is only at its default. The adapter
fills `spec.ExplicitFlags`, read through `FlagOf.IsSet`, from the framework's report of changed
flags, and the model stays free of any CLI framework (ADR-0005).

`version` keeps its local `--json`, because `update`'s preflight runs `<staged> version --json`
(ADR-0007, ADR-0009), and also writes JSON under `--output json`.

## Pinning the keys

`speccheck.AssertJSONKeys(t, v, goldenPath)` in `cli/spec/speccheck` marshals `v` and compares the
sorted dotted key paths, with every array element collapsed to `[]`, against a golden file in the
caller's `testdata`. A renamed, removed or added key fails the test, which is the prompt to decide
whether `schema` moves. It lives in `cli/spec` so any project can pin its own keys.

The golden file is rewritten when the environment variable `SPECCHECK_UPDATE` is `1`. It is not a
flag: `gochecknoglobals` is enabled in `cli/spec`, so a package-level flag variable is refused, and
`cli/core` registers `-update` in its `TestMain`, so a second flag of that name in the same test
binary panics.

## Considered options

- **A wrapper, `{"schema":1,"data":{...}}`.** Every reader gains a level of nesting, and the
  contract of ADR-0007, which is flat, would change shape with it. The flat object versions the keys
  in place.
- **A `version` key or field for the shape.** It collides with `version --json`'s own `version`
  key.
- **A compact mode that keeps `version --json` on one line.** A second writer path, and a flag to
  carry, to preserve whitespace that no consumer reads.
- **Leaving `version` off the writer.** Two producers of the same envelope drift: one writes
  `schema` by hand, the other through the function that rejects a collision.
- **Deferring `--output`.** The format would remain a boolean, and a later second format would
  change every command that reads it.
- **A `-update` flag in `speccheck`.** Refused by `gochecknoglobals` in `cli/spec`, and it collides
  with the flag `cli/core` already registers.
- **A helper local to `cli/core`'s tests.** Another project could not pin its own keys without a
  copy.

## What this changes in earlier ADRs

- **ADR-0007.** The `version --json` object is indented and written through the envelope; its keys
  and `schema` of `1` are unchanged.
- **ADR-0009.** `Output` carries a `Format`, and `--output` joins the standard flags beside `--json`.

## Consequences

- **A key change is a test failure first.** A command that pins its keys with `AssertJSONKeys` sees
  a renamed, removed or added key before a consumer does.
- **A command cannot forget `schema`.** The writer puts it first and refuses a value that sets it.
- **A command reads one question.** `inv.Output.Format()` answers for `--json` and `--output`
  alike.
- A change to `cli/spec` selects its dependents in CI (ADR-0006).
