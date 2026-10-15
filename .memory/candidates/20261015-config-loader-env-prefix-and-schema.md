---
about: "config.Load builds its Schema with an empty env prefix because Env(prefix, lookup) derives its own variable names from its own prefix; the Schema's prefix only feeds Field.Env() and the collision check"
saw:
  - source/config/config.go
  - source/config/env.go
  - source/config/schema.go
  - docs/adr/0012-configuration-is-a-provider-based-project.md
---

- `Load` calls `NewSchema(defaults, "")` (`source/config/config.go`). `Env` ignores the Schema's prefix and calls `envName(p.prefix, f.key)` per schema field (`source/config/env.go`), so the prefix lives on the provider and a custom provider cannot rely on `Field.Env()` carrying it.
- A shared prefix cannot create or remove a collision between two env names, which is why the empty prefix is safe for `NewSchema`'s collision check. Do not thread a prefix through `Load` without revisiting that.
- Environment variables are looked up per known field and never scanned (ADR-0012): `APP_A_B_C` is ambiguous between `a.b_c` and `a_b.c`.
- `Load` takes a struct, not a pointer; the schema rejects pointer, embedded and map fields (`source/config/schema.go`).
