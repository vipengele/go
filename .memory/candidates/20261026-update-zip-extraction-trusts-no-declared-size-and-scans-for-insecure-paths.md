---
about: "zip extraction reads only the selected entry, never trusts declared sizes, and scans names itself because zip.ErrInsecurePath depends on GODEBUG zipinsecurepath and names no entry"
saw:
  - source/update/extract_zip.go
  - source/update/extract_zip_test.go
---

- The central directory lets `extractZip` validate every entry before reading a body, so one pass suffices and the
  other entries are never decompressed; the budget applies to the selected entry alone.
- `archive/zip` checks a body against its declared size: a body that outgrows its declared size fails with
  `zip.ErrFormat` before the byte limit is reached, and one shorter than declared fails with `io.ErrUnexpectedEOF`.
  So `*ExtractLimitError` appears only when the declared size is at least limit+1 and the body really exceeds the
  limit. A wrong CRC (`zip.ErrChecksum`) is known only after the whole body is copied, so dst already holds bytes;
  `Config.Extract` discards the staged file.
- `zip.NewReader` returns `zip.ErrInsecurePath` with a usable reader, but only when GODEBUG `zipinsecurepath=0`, and
  the error names no entry. `scanZip` therefore applies `unsafeEntryName` to every name regardless of GODEBUG, and
  when `NewReader` reported the error it also refuses names `filepath.IsLocal` rejects (`zipLocalName`), attaching
  `zip.ErrInsecurePath` as the cause. A test for that mapping uses `t.Setenv`, so it cannot be parallel.
- An encrypted entry (flag 0x1) is rejected only when it is the selected one; an unselected encrypted entry is never
  read.
