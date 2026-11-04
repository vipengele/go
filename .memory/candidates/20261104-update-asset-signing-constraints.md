---
about: "update's only trust anchor is the unsigned checksums file; no signing or tumika prior art exists anywhere in the repo, and constraints for a signature scheme (ADR-0001, 0003, 0020, 0023, Config style) are listed"
saw:
  - source/update/download.go
  - source/update/checksums.go
  - source/update/config.go
  - source/update/.golangci.yml
  - source/update/go.mod
  - docs/adr/0001-each-project-is-a-go-module-released-by-its-own-prefixed-tag.md
  - docs/adr/0003-the-library-is-three-projects-cli-update-and-daemon.md
  - docs/adr/0020-a-download-returns-a-verified-archive-the-caller-never-sees-unverified.md
  - docs/adr/0023-the-install-script-is-rendered-by-update-installscript-around-the-completion-snippet.md
---
Explored for ADR issue #58 (signature scheme for release assets).

- Trust today: the checksums file "is trusted as it arrives", HTTPS not enforced (download.go:53-54, ADR-0020 "The digest check is the integrity guarantee"). Download order: fetchChecksum (download.go:90) then fetchArchive hashes while writing (:149-152) and compares (:165). A signature step would slot between fetchChecksum and the digest compare; Selection (select.go:~25-31) carries only Archive and Checksums assets, so a signature asset needs a new Selection field.
- install script (ADR-0023) verifies with sha256sum/shasum only, "Verification is never skipped", POSIX sh, curl/wget; a scheme needing a Go-only verifier or cosign binary cannot be checked by the script without a new tool dependency. Script ignores Config.Source/HTTPClient.
- Config style: zero value = production default, documented per field (config.go:42-); Validate collects every problem with validate.FieldError wrapping validate.ErrInvalid (config.go:~127-); resolve copies and fills defaults (config.go:~250). Func/seam fields for outside world. Resolved() exported so installscript shares defaults (ADR-0023).
- Deps: update's go.mod requires only cli/spec, cli/state, commons, x/mod (direct). ADR-0003 says each project is its own module so a consumer does not inherit others' graph. depguard in source/update/.golangci.yml bans cli/core and cobra outside updatecmd; no rule on crypto libs. Stdlib crypto/ecdsa, ed25519 add no module dep; sigstore-go would add a large graph.
- ADR-0001: "no registry upload and no publishing credential, because the tag is the publication" (docs/adr/0001-...md:~27). Release assets come from the consumer tool's own GoReleaser (asset/ has GoReleaserArchive/GoReleaserChecksum name translators); update's repo does not publish those assets.
- Prior art: grep -rniE "minisign|cosign|sigstore|tumika|ecdsa" over docs, source, .github, agentic, tools finds nothing relevant (only unrelated toolkit text, vanity-worker lockfile). .memory has no note on signing.
