# A download returns an archive only after its digest is verified

Status: extends ADR-0019, whose located URLs the download step requests, and ADR-0003, whose `update`
project owns the download.

`Config.Download` fetches a release's archive to a temporary file and returns it as an `*Archive`
once its SHA-256 digest matches the one the release's checksums file lists. Nothing the caller can
reach opens the archive before that comparison.

## The rules

- **Verification precedes any open by the caller.** `Download` writes the file, hashes it as it is
  written, compares the digest and only then returns the handle. A failure removes the file and
  returns an error, never an unverified `*Archive`.
- **The digest covers the bytes written.** The hash is fed by the same copy that writes the file.
  It does not cover a later change to the file on disk.
- **Tampering on disk is limited by mode and name, nothing more.** The file is created with mode 0600
  and a random name. It is not claimed to be unswappable: a process running as the same user, or
  root, can still replace it.
- **The handle is narrow.** `Archive` exposes `Read`, `ReadAt`, `Seek`, `Size`, `Name` and `Close`.
  `Close` closes the file, then removes it, and is idempotent. A caller cannot close without removing.
- **The temp location is a `Config` field.** `TempDir` empty means `os.TempDir()`. A missing
  directory is an error and is not created.
- **The download is bounded.** The archive is capped by `MaxDownloadBytes` (default 256 MiB), the
  checksums file by 1 MiB, and each request by `DownloadTimeout` (default 5 minutes). Sizes announced
  by `Content-Length` or `Asset.Size` are hints; the digest decides.
- **The digest check is the integrity guarantee.** HTTPS is not enforced and the checksums file is
  trusted as it arrives.

## Considered options

- **Returning a path in a caller-owned directory.** Verification becomes a convention: the caller
  can open or swap the file before or after the check, and nothing in the API stops it.
- **A callback that owns the file's lifetime.** The file could not outlive the call, so the steps
  that read the archive afterwards, extraction and replacement of the executable, would have to run
  inside the callback.
- **`os.TempDir()` as the only location.** Tests would need `t.Setenv` to redirect it, which
  forbids `t.Parallel`.
- **The executable's directory.** It needs write access before anything is verified and leaves
  archives behind in install directories.

## Consequences

- Extraction and replacement steps read an `*Archive` that is already verified, through `ReaderAt`
  and `Size`, and own closing it.
- A caller that never closes the `*Archive` leaves the temp file behind.
- A tool that wants the archive somewhere other than `TempDir` sets `TempDir`; the directory must
  exist and be writable.
