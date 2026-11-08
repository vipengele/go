---
about: "update supports Windows end to end: zip extraction and .exe lookup, the two-rename Install with a .old backup, RemoveStaleBackup from the check hook on Windows, zip as the Windows default asset extension, and a PowerShell installer; decisions are in ADR-0030 and ADR-0031"
saw:
  - docs/adr/0030-windows-tests-run-for-projects-with-windows-files.md
  - docs/adr/0031-update-on-windows.md
  - source/update/install.go
  - source/update/stale.go
  - source/update/asset/asset.go
  - source/update/installscript/render.go
  - source/update/install_windows_test.go
---

- Windows pieces and where they live: zip extraction (extract_zip.go), `<name>.exe` lookup
  (extract.go `binaryFileName`), two-rename Install keeping `<exe>.old` (install.go, ADR-0022),
  `*BackupLockedError` (install.go `backupLocked`), `Config.RemoveStaleBackup` (stale.go),
  `asset.DefaultExtFor` (asset/asset.go), `installscript.RenderPowerShell` (render.go,
  install.ps1.in), `completioninstall.PowerShellSnippet` (source/cli/spec).
- `install_windows_test.go` installs over a running helper exe (testdata/waitforstdin) in a path
  with a space; it runs only on the gated windows-latest job (ADR-0030). Lydite coverage is Linux
  only, so every Windows branch also has a Linux test with an injected fault or an injected goos.
- ADR-0031 records the Windows decisions and the alternatives each rejects.
