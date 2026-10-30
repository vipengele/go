package update

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
)

// zipFlagEncrypted is the general-purpose flag bit of a zip entry whose body is encrypted.
const zipFlagEncrypted = 0x1

// extractZip is the extractFunc of a zip archive. The binary is the one regular file whose base
// name is name, at any depth; a directory of that name is not it. No match is a
// [*BinaryNotFoundError], more than one a [*MultipleBinariesError], an encrypted match an
// [*UnsafeEntryError], and a match of zero bytes [ErrEmptyAsset].
//
// Every entry of the central directory is checked before any body is read, and any one entry the
// policy of [zipEntryCheck] refuses rejects the whole archive with an [*UnsafeEntryError], wherever
// it sits relative to the binary, so nothing is written to dst for a rejected archive.
//
// Only the selected entry's body is read, so limit applies to it alone and a compression bomb in
// any other entry is never decompressed. The sizes the archive declares are not trusted: limit is
// enforced on the bytes the entry actually decompresses to, past it an [*ExtractLimitError] for
// the entry. A body that disagrees with its declared size or checksum fails with the error
// [archive/zip] reports, such as [zip.ErrFormat], [io.ErrUnexpectedEOF] or [zip.ErrChecksum], and
// a compression method it does not support is [zip.ErrAlgorithm]. Bytes that are not a zip
// archive, or one cut short, are [zip.ErrFormat].
func extractZip(ctx context.Context, src io.ReaderAt, size int64, name string, limit int64, dst io.Writer) error {
	zr, err := zip.NewReader(src, size)
	insecure := errors.Is(err, zip.ErrInsecurePath)

	if err != nil && !insecure {
		return fmt.Errorf("update: reading zip directory: %w", err)
	}

	f, err := scanZip(ctx, zr.File, name, insecure)
	if err != nil {
		return err
	}

	return copyZipEntry(ctx, f, limit, dst)
}

// scanZip checks every entry of files against the entry policy and returns the one regular file
// named name. insecure is whether [zip.NewReader] reported [zip.ErrInsecurePath]: the entry it
// refers to is then the first entry refused, with that error as its cause, and an archive in which
// no entry is refused is rejected with an [*UnsafeEntryError] that has no name.
func scanZip(ctx context.Context, files []*zip.File, name string, insecure bool) (*zip.File, error) {
	var (
		matches []string
		match   *zip.File
	)

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if err := zipEntryCheck(f, insecure); err != nil {
			return nil, err
		}

		if f.Mode().IsRegular() && path.Base(f.Name) == name {
			matches = append(matches, f.Name)
			match = f
		}
	}

	switch {
	case insecure:
		return nil, &UnsafeEntryError{Reason: "not a local path", Err: zip.ErrInsecurePath}
	case len(matches) == 0:
		return nil, &BinaryNotFoundError{Name: name}
	case len(matches) > 1:
		return nil, &MultipleBinariesError{Name: name, Entries: matches}
	case match.Flags&zipFlagEncrypted != 0:
		return nil, &UnsafeEntryError{Name: match.Name, Reason: "encrypted"}
	}

	return match, nil
}

// zipEntryCheck applies the entry policy to f, returning an [*UnsafeEntryError] for an entry it
// refuses. Accepted are a regular file and a directory, whose name ends in a slash or whose mode
// says so. Refused are a symbolic link, whatever its target, because it can stand for the binary or
// reach outside the archive; any other mode that is not a regular file or a directory, such as a
// device, a named pipe or a socket; and a name that fails [unsafeEntryName]. When insecure is
// true, [zip.ErrInsecurePath] is the cause of whichever refusal is returned.
func zipEntryCheck(f *zip.File, insecure bool) error {
	var cause error
	if insecure {
		cause = zip.ErrInsecurePath
	}

	switch t := f.Mode().Type(); t {
	case 0, fs.ModeDir:
	case fs.ModeSymlink:
		return &UnsafeEntryError{Name: f.Name, Reason: "symbolic link", Err: cause}
	default:
		return &UnsafeEntryError{
			Name:   f.Name,
			Reason: fmt.Sprintf("mode %v is not a regular file or a directory", t),
			Err:    cause,
		}
	}

	if reason, unsafe := unsafeEntryName(f.Name); unsafe {
		return &UnsafeEntryError{Name: f.Name, Reason: reason, Err: cause}
	}

	return nil
}

// copyZipEntry copies the body of f to dst through a [ctxReader], writing at most limit bytes. A
// body of zero bytes is [ErrEmptyAsset].
func copyZipEntry(ctx context.Context, f *zip.File, limit int64, dst io.Writer) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("update: opening %q: %w", f.Name, err)
	}

	defer func() { _ = rc.Close() }()

	w := &countingWriter{w: dst}
	if err := copyBounded(w, &ctxReader{ctx: ctx, r: rc}, f.Name, limit); err != nil {
		return err
	}

	if w.n == 0 {
		return fmt.Errorf("%w: archive entry %q", ErrEmptyAsset, f.Name)
	}

	return nil
}

// countingWriter writes to w and counts the bytes written.
type countingWriter struct {
	w io.Writer
	n int64
}

// Write implements [io.Writer].
func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err
}
