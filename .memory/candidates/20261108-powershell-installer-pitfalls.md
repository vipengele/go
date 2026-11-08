---
about: "The PowerShell installer runs under irm | iex, so it is one & { } block that throws instead of exit; 5.1's Expand-Archive needs a .zip name, tar.exe is called by its System32 path, and the user PATH is written raw with its own registry kind"
saw:
  - source/update/installscript/install.ps1.in
  - source/update/installscript/extract-zip.ps1.in
  - source/update/installscript/extract-tar.ps1.in
  - source/update/installscript/script_windows_test.go
---

- `exit` under `irm | iex` closes the caller's window, so the script is one `& { ... }` block and
  `Fail` throws "<name>: error: ..."; there is no `param()` block.
- Native commands run through `InvokeTool` with a local `$ErrorActionPreference = 'Continue'`:
  under Stop, Windows PowerShell 5.1 turns redirected stderr into a terminating error.
- extract-zip.ps1.in renames the download to `asset.zip`: 5.1's Expand-Archive refuses any other
  extension. extract-tar.ps1.in calls `[Environment]::SystemDirectory\tar.exe` because Git for
  Windows' GNU tar on PATH reads `C:` as a remote host.
- `AddToPath` reads `HKCU:\Environment` Path with `DoNotExpandEnvironmentNames` and writes it back
  with `New-ItemProperty -PropertyType` of its existing kind (a missing value becomes
  ExpandString); `[Environment]::SetEnvironmentVariable(...,'User')` would write REG_SZ.
- The registry PATH test in script_windows_test.go runs only when GITHUB_ACTIONS=true, never in
  parallel, and restores the raw value in t.Cleanup.
