---
about: "ADR-0024 (merged, aa8d213 #165) already decides signing for update: minisign over the checksums file, TrustedKeys, fail-closed in Download; issue #59 only implements it"
saw:
  - docs/adr/0024-release-assets-are-authenticated-by-a-minisign-signature-over-the-checksums-file.md
  - source/update/download.go
  - source/update/go.mod
  - docs/release-notes
---
Explored for planning #59 (sub-issue of #9; #58 "Choose the signature scheme" is closed and produced ADR-0024).

- No code yet: grep -rniE "TrustedKeys|minisig|ed25519" source -> 0 hits; only the ADR mentions them. go.mod has no x/crypto (needs adding, ADR-0024 "Consequences").
- Download order today: fetchChecksum (download.go:81, parses digest) -> fetchArchive (:86, hash while writing :149-152, compare :165). The signature must be verified over the raw checksums bytes before parseChecksum (download.go:100) runs, so fetchChecksum must buffer the (1 MiB-capped) body, not stream-parse it. Verification before open/stage is already structurally true: Download is before Extract/Install (ADR-0020, 0021, 0022).
- docs/release-notes holds only commons/v0.1.0.md: update has no release note and (git tag -l 'source/*' printed nothing) no tag, so it is unreleased; no repo project requires go.vipengele.org/update (grep over source go.mod/*.go: none; daemon go.mod does not list it). Making keys/fail-closed mandatory is not breaking, though ADR-0024 deliberately keeps empty TrustedKeys = checksum only.
- Release side: this repo publishes no binaries; consumers' GoReleaser does (ADR-0001, ADR-0024). Nothing in .github/workflows signs anything; the only "sign|checksum" hit is ci-orchestration.yml, unrelated to release assets.
- Issue #59 criteria vs ADR-0024: "consumer can supply its own" = Config.TrustedKeys; ADR forbids env/flag key sources. Downgrade/replay is explicitly out (#61).
- Not re-checked: GitHub sub-issue bodies other than 9/58/59.
