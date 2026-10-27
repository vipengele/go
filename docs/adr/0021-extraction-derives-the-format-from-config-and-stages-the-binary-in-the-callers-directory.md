# Extraction derives the format from the config and stages the binary in the caller's directory

Status: extends ADR-0020, whose verified archive the extraction step consumes.

`Config.Extract` turns a verified `*Archive` into a `*Staged` binary: a new file, mode 0700 and
synced, that the caller renames into place or discards. The archive's bytes are untrusted input to
the extractor even though their digest matched, because the digest vouches for the release's
checksums file, not for the archive's contents.

## The rules

- **The format is derived from the `Config`, never from the bytes.** An `AssetTemplate` without
  `{ext}` names a raw binary. Otherwise `AssetExt` decides: `tar.gz` or `tgz` is a gzip-compressed
  tar and `zip` is a zip, matched exactly in lower case. An empty template or extension counts as
  `asset.DefaultTemplate` or `asset.DefaultExt`. `Validate` rejects any other `AssetExt` when the
  template holds `{ext}`, so a misconfigured tool fails before it downloads anything.
- **The staged file lives in a directory the caller supplies.** `Extract` takes `dir`, which has no
  default and must exist. The file is created there with `os.CreateTemp(dir, TempPrefix+"*")`, so
  the caller chooses the directory of the file it will rename into place and the rename stays on one
  filesystem. `Config.TempDir` is for the download only.
- **One unsafe entry rejects the whole archive.** Every entry is checked before anything is written.
  An absolute path, a `..` component, a backslash, a drive letter, a NUL byte, a symbolic link, a
  hard link or any entry that is neither a regular file nor a directory is an `*UnsafeEntryError`,
  wherever it sits relative to the binary. The binary is the regular file whose base name is
  `BinaryName`, plus `.exe` when `GOOS` is `windows`, at any depth. None is a `*BinaryNotFoundError`,
  several a `*MultipleBinariesError`, an empty one `ErrEmptyAsset`. The staged file is mode 0700
  whatever mode the archive records.
- **Extraction has its own budget.** `Config.MaxExtractBytes` (default 512 MiB) gives an
  `*ExtractLimitError`. For tar.gz one cumulative cap covers the whole decompressed stream, headers
  and skipped bodies included, because `tar.Reader.Next` decompresses every body before the next
  header, so a bomb in an entry the extractor skips costs as much as one it copies. For zip and raw
  the cap applies to the selected entry or file alone and counts the bytes actually read; the other
  zip entries are never read. A tar.gz is read twice, a validating pass and then the copy, so that
  nothing is written for a rejected archive.
- **`Extract` owns the archive.** It closes the `*Archive`, removing its temporary file, on success
  and on every failure, so the caller has nothing to clean up in either case.
- **A `*Staged` is settled once.** `Keep` closes the file and leaves it on disk; `Discard` closes and
  removes it. Whichever is called first decides and later calls do nothing.

## Considered options

- **Sniffing the format from magic bytes.** The bytes come from the network, so the attacker would
  choose the parser, and a mismatch between the published name and the contents would go unnoticed.
  The configured name already says what the release publishes.
- **A default staging directory, or a destination the extractor owns.** A default cannot know which
  filesystem the replacement will rename onto, and a file in the wrong one makes the rename a copy.
  The caller knows its install directory.
- **`Config.TempDir` for the staged file.** The temp directory is commonly a different filesystem
  from the install directory, and a rename across filesystems fails or copies, which breaks atomic
  replacement.
- **Reusing `MaxDownloadBytes` or `SizeLimitError`.** A download cap bounds compressed bytes on the
  wire; the bytes written after decompression need their own cap and their own error to tell the
  two failures apart.
- **Skipping unsafe entries and extracting the rest.** An archive that holds a link or a traversal
  path is malformed or hostile, and extracting around it trusts the rest of what it holds. Rejecting
  it whole is also order-independent.
- **An empty `AssetExt` meaning raw.** An empty `AssetExt` resolves to `tar.gz`, as every empty
  field resolves to its default. A raw binary is written by leaving `{ext}` out of the template.
- **Trusting zip's declared sizes.** The central directory is attacker-controlled, so a declared
  size cannot bound the output. The cap counts bytes actually decompressed.
