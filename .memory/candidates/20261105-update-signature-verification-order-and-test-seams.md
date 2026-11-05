---
about: "update verifies a minisign signature over the buffered checksums file before parsing it or requesting the archive, fails closed unless AllowUnsigned, and its tests need the server's own key"
saw:
  - source/update/download.go
  - source/update/config.go
  - source/update/select.go
  - source/update/installscript/install.sh.in
  - source/update/installscript/render.go
  - source/update/installscript/script_test.go
  - source/update/installscript/harness_test.go
  - source/update/updatetest/server.go
  - docs/adr/0026-a-release-is-refused-unsigned-unless-the-consumer-opts-out.md
---
- Policy: Config.Validate requires exactly one of TrustedKeys and AllowUnsigned (checkSignaturePolicy, config.go), so `update.Config{}` fails every operation, Check included, because resolve validates. ADR-0026 records why; ADR-0024's "empty TrustedKeys = checksum only" no longer holds.
- Download order (download.go): fetchBounded reads the checksums file in full under 1 MiB, verifySignature fetches `<checksums>.minisig` under maxSignatureBytes (4 KiB) and runs internal/minisign.Verify, and only then parseChecksum and fetchArchive. With keys set, a hand-built Selection whose Signature is zero fails closed with errNoURL rather than skipping verification.
- Selection.Signature is the zero Asset under AllowUnsigned (select.go); on the listed path a missing signature is joined into Select's AssetNotFoundError, so Download is never reached.
- Install script: the check lives between `@@SIGNED_BEGIN@@`/`@@SIGNED_END@@` lines in install.sh.in; render.go's signedRegions drops those regions whole under AllowUnsigned, so that script is byte-identical to an unsigned one. Keys reach the script as minisign.PublicKey.Line only.
- Test seams: every updatetest.Server signs with a fresh random key (server.go signChecksums), so a Config must use srv.PublicKey(). The installscript harness renders its script before the server exists; script_test.go's newSignedHarness re-renders with the server's key, and a keyed test that uses plain newHarness fails verification. The harness links a minisign stand-in (testdata/fakeminisign) by path, and its pty puts the slave on the script's stderr, so prompts land in runResult.TTY, not Stderr.
