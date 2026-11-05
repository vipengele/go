# A release is refused unsigned unless the consumer opts out

Status: amends ADR-0024, whose verification applies only when `TrustedKeys` is non-empty, and its
install script rule, which continues without `minisign`.

A signature that is checked only when a tool remembers to configure keys protects no tool that
forgot. An `update.Config` that names no key and says nothing else would update on the checksums
file alone, which proves integrity and not authorship (ADR-0024). `update` therefore requires a
decision: either keys, or an explicit statement that the tool's releases are unsigned.

## The rules

- **Keys or an opt-out, never neither.** `Config` gains `AllowUnsigned bool` beside
  `TrustedKeys []string`. `Validate` fails when `TrustedKeys` is empty and `AllowUnsigned` is
  false, and fails when both are set: a tool that lists keys and also allows unsigned releases
  states two contradictory policies, and neither is silently preferred.
- **`update.Config{}` is invalid for every operation.** `resolve` runs `Validate`, so `Check` and
  `CheckManaged` fail the same way `Download` does. A tool cannot learn that an update exists under
  a policy it would refuse to apply.
- **A `Selection` lists exactly what `Download` reads.** Under `AllowUnsigned`,
  `Selection.Signature` is the zero `Asset`; otherwise it is the rendered signature asset. With
  `AllowUnsigned` no signature is fetched and the checksum is the only check, as ADR-0020
  describes.
- **The library has no environment opt-out.** `AllowUnsigned` and the keys are set where the tool
  builds its `Config`, in the consumer's code. Keys come only from `Config.TrustedKeys`; several
  may be listed, for the rotation ADR-0024 describes. No environment variable, flag or
  configuration key sets either field.
- **The rest of ADR-0024 stands.** With keys, `Download` stays fail-closed on a missing, unreadable,
  unknown-key or bad signature.

## The install script

`installscript.Render` inherits `Validate`, so a `Config` that cannot update cannot render a script.

- **Keys are embedded as parsed lines.** Each trusted key is rendered as its 56-character key
  line, never the raw `TrustedKeys` entry, so an `untrusted comment:` line or stray whitespace
  cannot reach the shell.
- **With `AllowUnsigned` the script is unchanged:** it checks the digest only.
- **With keys the script fetches `<checksums>.minisig`.** A 404 fails the install.
- **When `minisign` is on `PATH`** the script runs `minisign -V -P <key> -m <checksums> -x <sig>`
  once per key and fails if none verifies.
- **When `minisign` is absent** the script warns on stderr that the signature cannot be checked,
  then decides:
  1. `<PREFIX>_ALLOW_UNSIGNED=1` proceeds on the checksum alone.
  2. Otherwise, if `/dev/tty` is usable, the script prompts there and only `y` or `yes` proceeds;
     `no`, anything else and end of input refuse with a message.
  3. Without a usable tty the script fails and names both remedies: install `minisign`, or set
     `<PREFIX>_ALLOW_UNSIGNED=1`.

  The prompt reads `/dev/tty` because under `curl | sh` standard input is the script itself.

### Why the script may take an environment opt-out and the library may not

A key from the environment lets whoever controls the environment make the tool trust a release of
their own. `<PREFIX>_ALLOW_UNSIGNED` cannot: it only lowers verification to checksum-only, on the
user's own machine, after a warning, and never makes a foreign release trusted. Whoever can set it
already runs the script and chooses what it installs. The library's `AllowUnsigned` is the tool
author's statement about the tool's own releases; the script's variable is the installing user's
statement about their own machine.

## Considered options

- **ADR-0024's opt-in default, verifying only when keys are set.** Rejected: an unconfigured
  `Config` is the common case of a forgotten decision, and it fails open to an unauthenticated
  update with nothing in the code to show that it was chosen.
- **Fail-closed with no opt-out.** Rejected: a tool whose releases are not signed, or not yet,
  could not use `update` at all, and would be pushed to a dummy key that verifies nothing real.
- **Keys win when both are set.** Rejected: the second field is then silently ignored, so a stray
  `AllowUnsigned` looks like a policy and is not one. Rejecting the combination keeps the `Config`
  readable as written.
- **A sentinel key value as the opt-out.** Rejected: it hides a policy decision inside a list of
  keys, passes through key parsing and rotation logic as a special case, and could be set by
  anything that fills the list.
- **A script that warns and proceeds without `minisign`.** Rejected: a warning nobody reads under
  `curl | sh` leaves the install as unauthenticated as having no keys, for exactly the users who
  lack the tool.
- **A script that refuses with no override.** Rejected: a first install on a machine without
  `minisign` would be impossible, and the user, who chose to run the script, has no way to accept
  the checksum-only guarantee for themselves.
- **A script that proceeds when there is no tty.** Rejected: with no one to ask, proceeding is the
  same as warning and continuing; a non-interactive install must fail and name its remedies, so
  that checksum-only is always a choice made by a person or by the variable.

## Consequences

- Every tool built on `update` sets `TrustedKeys` or `AllowUnsigned` in its `Config`; one that
  sets neither fails at `Validate`, in every operation.
- ADR-0024's "with `TrustedKeys` empty no signature is fetched" holds only with `AllowUnsigned`
  set.
- ADR-0024's install script, which says on stderr that the signature was not checked and continues,
  now stops there unless the user allows it through the variable or the prompt.
- `Selection` carries a zero `Signature` under `AllowUnsigned`, so a caller that reads the asset
  list sees the same assets `Download` fetches.
