# Release assets are authenticated by a minisign signature over the checksums file

Status: amends ADR-0020, whose checksums file is trusted as it arrives, and ADR-0023's install
script, whose digest check is its only guarantee. Amended by ADR-0026, which makes a release
refused unsigned unless the consumer opts out, in place of verifying only when keys are set, and by
ADR-0027, which refuses a version below the highest applied or running.

A checksums file fetched from the same release as the archive proves the archive arrived intact,
not that the tool's author published it: whoever can replace the archive can replace the checksums
file beside it. `update` authenticates a release by a detached minisign signature over the checksums
file, checked against public keys the tool compiles in. The signed checksums file then vouches for
every archive it lists, so one signature per release covers every platform.

The keys belong to a tool released with `update`, not to this repository. This repository's own
projects are Go modules, authenticated by the module proxy and the checksum database; nothing here
signs them, and ADR-0001's release workflow is unchanged.

## The rules

- **What is signed is the checksums file.** The tool's release publishes the signature as the asset
  named by the rendered `ChecksumsAsset` followed by `.minisig` (`checksums.txt.minisig` by
  default). The suffix is fixed, not a template: the signature is always found beside the file it
  signs.
- **The format is minisign's.** A signature file is four lines: an untrusted comment, the base64
  of the algorithm (`ED`, Ed25519 over the file's BLAKE2b-512 hash, or `Ed`, Ed25519 over the file
  itself), the 8-byte key id and the 64-byte signature, a trusted comment, and the base64 of the
  global signature, Ed25519 over the signature followed by the trusted comment's text. Both
  signatures must verify. The trusted comment is authenticated but not interpreted.
- **A key is a minisign public key.** An entry of `Config.TrustedKeys []string` is the base64 line
  `minisign -G` writes as the second line of `minisign.pub`: `Ed`, the 8-byte key id and the 32-byte
  Ed25519 public key, 56 characters starting `RW`. An entry may carry the file's
  `untrusted comment:` line before it, so a tool can `//go:embed` its `minisign.pub` as is.
  `Validate` reports an entry that does not parse, and two entries with one key id, as
  `TrustedKeys[i]`.
- **The signature's key id picks the key.** The key in `TrustedKeys` whose id matches the
  signature's verifies it. An id no key carries is a typed error naming the id, distinct from a
  signature that does not verify, so a tool that missed a rotation says which key it lacks.
- **Keys come from code only.** `TrustedKeys` is set where the tool builds its `Config`, from
  constants or an embedded file. No environment variable, flag or configuration key adds or
  replaces a key: anyone who can write those could otherwise make the tool trust their own release.
- **Fail-closed once keys are set.** With `TrustedKeys` non-empty, `Download` fetches the signature
  with the checksums file and verifies it before parsing a digest out of the checksums file. A
  missing signature asset, an unreadable one, an unknown key id and a bad signature each refuse the
  update; no archive is fetched and nothing is staged. With `TrustedKeys` empty no signature is
  fetched and the checksum is the only check, as ADR-0020 describes.
- **The signature is bounded.** The signature asset is read up to 4 KiB, a few times the 300-odd
  bytes minisign writes, under the same `DownloadTimeout` as the checksums file. On the redirect path
  its URL comes from the `AssetLocator`, and a 404 is the `AssetNotFoundError` of ADR-0019.
- **The install script verifies when it can.** A script rendered from a `Config` with keys embeds
  them and runs `minisign -V` on the checksums file when `minisign` is on `PATH`, failing on a bad
  signature. Without `minisign` it says on stderr that the signature was not checked and continues
  on the digest. POSIX sh has no Ed25519, and a first install should not require a tool the user
  does not have; the script itself comes from the same origin the user already chose to trust.

## Rotation

A tool compiles in its current key and, from its first signed release, the public half of a
successor kept offline. Any key in the list verifying is enough.

- **A routine rotation** starts signing with the successor, then a later release adds the next
  successor and drops the retired key. Every install already trusts the successor, so none is
  stranded.
- **A compromised key** is dropped in a release signed by the successor. Installs that still trust
  the compromised key accept a release signed by it until they update past that release; a
  signature scheme with a compiled-in list cannot revoke faster than the tool updates.
- **A rotation to a key no install knows** is a release signed by the new key that every earlier
  install refuses with the unknown-key error. Those installs reinstall through the script. The
  successor key exists to make this the exception.

## Considered options

The candidates were judged on four criteria: no extra publishing credential (ADR-0001), key
rotation, offline verification, and dependency weight added to every tool that requires `update`.

| | Publishing credential | Rotation | Offline verification | Dependency weight |
|---|---|---|---|---|
| **minisign over the checksums file** | a stored secret key | compiled-in list with a successor | yes | `golang.org/x/crypto` for BLAKE2b |
| **Sigstore keyless (cosign, GitHub artifact attestations)** | none: the CI's OIDC identity | none for the tool; Sigstore rotates its own roots | no: needs a fresh trust root | sigstore-go, go-tuf, protobuf and their graph |
| **Signed manifest (tumika's ECDSA P-256 over a release BOM)** | a stored secret key | compiled-in list | yes | standard library only |

- **Sigstore keyless, through cosign or GitHub artifact attestations.** It is the only candidate
  that needs no stored secret: the release workflow's OIDC token gets a short-lived certificate,
  and the verifier checks the workflow identity instead of a key. Rejected because the trust moves
  to Sigstore's roots, which rotate on Sigstore's schedule: a binary verifies with whatever root it
  has, so it either fetches a fresh one through TUF on every check, which is network access to a
  third service on the update path, or compiles one in and stops being able to update the day the
  root it carries is retired, with no path back but a reinstall. Its verifier, sigstore-go, brings
  TUF, protobuf and transparency-log clients into the module graph of every tool, against ADR-0003's
  reason for a project per module. The shell install script cannot verify it without `cosign`.
- **A signed manifest, as tumika does.** ECDSA P-256 over a JSON bill of materials naming every
  asset's URL and digest verifies with the standard library alone. Rejected because the manifest is
  a second document beside the checksums file that GoReleaser already writes, in a format every
  tool's release pipeline would have to produce with a signer of its own, and release channels are
  to be expressed through GitHub's pre-release flag and the tag, not a manifest. Signing the checksums file gives the same
  guarantee, since the file already maps each asset name to its digest. A bare ECDSA signature over
  the checksums file was also weighed: it needs no dependency, but has no key id, no tool to
  produce or check it outside Go, and nothing an install script can call.
- **minisign, chosen.** It gives up the first criterion: the tool's release pipeline holds one
  secret, the minisign secret key. That is the accepted cost, kept small by holding the key as a
  secret of a protected `release` environment that only tag pushes reach. In return
  the update path verifies offline against keys the tool controls, the format is one a user can
  check by hand with `minisign -Vm checksums.txt -P <key>`, GoReleaser's `signs` section produces
  it, and the verifier is about a hundred lines over `crypto/ed25519` and
  `golang.org/x/crypto/blake2b`. `golang.org/x/crypto` is maintained by the Go team, as the
  `golang.org/x/mod` `update` already requires is; a third-party minisign library would add a
  module for the same hundred lines.

## Consequences

- `update` requires `golang.org/x/crypto`. It parses minisign keys and signatures itself and has no
  signing code; tests sign fixtures with an Ed25519 key they generate, through `updatetest`.
- `Config` gains `TrustedKeys`. `Selection` gains the signature asset's name, rendered from
  `ChecksumsAsset`, so a caller sees the three assets a download reads.
- ADR-0020's "the checksums file is trusted as it arrives" holds only for a `Config` with no
  `TrustedKeys`. With keys, the checksums file is trusted once its signature verifies, and the
  digest rule then carries that trust to the archive.
- A tool's release pipeline signs the checksums file, for instance with GoReleaser and a key made by
  `minisign -G -W`, which has no password, written from the environment secret to `minisign.key`:

  ```yaml
  signs:
    - artifacts: checksum
      cmd: minisign
      args: ["-S", "-s", "minisign.key", "-m", "${artifact}", "-x", "${signature}"]
      signature: "${artifact}.minisig"
  ```

- Downgrade and replay are not covered: an attacker who serves an older, correctly signed checksums
  file with its archives passes this check. Refusing a lower version than the highest ever applied
  is the release-channel decision's.
