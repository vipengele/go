---
about: "tar.gz extraction caps the whole decompressed stream because tar.Reader.Next reads through skipped bodies, reads the archive twice so nothing is written for a rejected one, and checks the gzip magic itself"
saw:
  - source/update/extract_tar.go
  - source/update/extract_tar_test.go
  - source/update/errors.go
---

- `tar.Reader.Next` decompresses every preceding body, so a bomb in an entry that is never selected is only caught
  by a cap on the whole decompressed stream: `budgetReader` in extract_tar.go sits between gzip and the tar reader
  and counts headers, padding and trailing bytes too. The limit error can surface inside `Next` wrapped, so
  `tarGzError` unwraps an `*ExtractLimitError` with `errors.As` before anything else.
- Policy needs every entry seen before any byte is written, so `extractTarGz` scans the archive (pass 1, recording the
  matching entry's index and draining to the gzip trailer) and then re-reads it to copy that entry (pass 2). Each
  pass has its own cumulative budget.
- `gzip.NewReader` reads ten bytes before it checks the magic, so a short non-gzip input would read as a truncated
  header; `checkGzipMagic` tests the first two bytes itself to return `ErrNotGzip`. `io.ErrUnexpectedEOF` from gzip
  or tar is `ErrTruncatedGzip`. Fewer than ten bytes of garbage after a complete stream also reads as truncated
  (the multistream reader cannot tell it from a cut second member); ten or more is `gzip.ErrHeader`.
- `archive/tar` cannot produce a NUL in a name (a ustar name ends at its first NUL; a PAX `path` with NUL is
  `tar.ErrHeader`), so the NUL test hand-builds 512-byte headers with valid checksums.
