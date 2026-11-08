---
about: "installscript.Render resolves with GOOS linux and RenderPowerShell with GOOS windows when the Config names none, because AssetExt defaults from GOOS (zip on windows) and the sh script refuses zip"
saw:
  - source/update/installscript/render.go
  - source/update/asset/asset.go
  - source/update/config.go
---

- `resolve(cfg, goos)` in render.go sets GOOS on a copy only when empty, then calls
  `Config.Resolved`; defaults stay in update. Without it, rendering the sh script on a Windows host
  resolves AssetExt to zip and fails with `ErrZipAsset`, so the script would depend on the machine
  that renders it.
- An explicit GOOS "windows" passed to Render still fails with ErrZipAsset; an explicit non-Windows
  GOOS passed to RenderPowerShell gives a tar.gz default, which the ps1 script handles via tar.exe.
- `asset.DefaultExtFor(goos)` is the only source of the default; `asset.DefaultExt` does not exist.
  config.go `resolve` sets GOOS before the AssetExt default.
