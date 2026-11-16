---
about: "the Windows SCM adapter calls windows.CreateService itself because mgr.Mgr.CreateService quotes its executable path, and opens services with query-only rights for Status because mgr asks for ALL_ACCESS"
saw:
  - source/daemon/service/scm_windows.go
  - source/daemon/service/windows.go
---

- The driver builds the whole command line with `windowsEscapeArg` (windows.go, the rules of
  `syscall.EscapeArg`) and hands it to `create` as `BinaryPathName`. `mgr.Mgr.CreateService` takes
  an executable path and quotes it, which would quote the command line a second time, so
  `windowsSCMManager.create` (scm_windows.go) calls `windows.CreateService` directly and then
  writes Description, SidType and DelayedAutoStart through `UpdateConfig`.
- `mgr.Service.UpdateConfig` passes `BinaryPathName` raw (empty means no change) and always rewrites
  SidType, DelayedAutoStart and Description, which is why a re-install is read-modify-write through
  `Config()` (windows.go `register`).
- `mgr.Connect` and `mgr.Mgr.OpenService` ask for SC_MANAGER_ALL_ACCESS and SERVICE_ALL_ACCESS,
  which an unelevated process is denied. `Status` connects with `scmQuery`: `OpenSCManager` with
  SC_MANAGER_CONNECT and `OpenService` with SERVICE_QUERY_CONFIG|SERVICE_QUERY_STATUS, so it needs
  no elevation.
