package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// stagedMode is the permission of a staged binary: readable, writable and runnable by its owner
// alone, whatever mode the archive records for the entry.
const stagedMode os.FileMode = 0o700

// errNoArchive reports a nil [*Archive] passed to [Config.Extract].
var errNoArchive = errors.New("update: no archive to extract")

// errNoStagingDir reports an empty directory passed to [Config.Extract].
var errNoStagingDir = errors.New("update: no directory to stage the binary in")

// Staged is a binary extracted from a verified [Archive]: an open file, synced to disk, in the
// directory [Config.Extract] was given. Exactly one of [Staged.Keep] and [Staged.Discard] settles
// it; whichever is called first decides, and every later call of either does nothing and returns
// nil.
//
// A Staged is not safe for concurrent use.
type Staged struct {
	f *os.File
	// remove deletes the file by name. Extract sets it to [os.Remove].
	remove  func(string) error
	settled bool
}

// Name is the path of the staged binary.
func (s *Staged) Name() string {
	return s.f.Name()
}

// Keep closes the staged binary and leaves it on disk, so that it can be renamed into place: a
// file is closed before it is renamed because Windows refuses to rename an open file. It returns
// the error closing the file. Once Keep or Discard has been called, Keep does nothing and returns
// nil.
func (s *Staged) Keep() error {
	if s.settled {
		return nil
	}
	s.settled = true
	return wrapNonNil("update: closing staged binary", s.f.Close())
}

// Discard closes the staged binary and removes it, returning both errors joined. The file is
// closed before it is removed because Windows refuses to remove an open file. Once Keep or Discard
// has been called, Discard does nothing and returns nil, so a kept file is never removed by it.
func (s *Staged) Discard() error {
	if s.settled {
		return nil
	}
	s.settled = true
	closeErr := s.f.Close()
	removeErr := s.remove(s.f.Name())
	return errors.Join(
		wrapNonNil("update: closing staged binary", closeErr),
		wrapNonNil("update: removing staged binary", removeErr),
	)
}

// Extract writes the binary a verified archive holds to a new file in dir and returns it as a
// [*Staged]. Extract owns a: it closes a, removing its temporary file, before it returns, whether
// it succeeds or fails, and an error closing it is joined onto the error returned. A failure to
// close a after a successful extraction discards the staged binary and returns that error.
//
// The Config is resolved first, so an invalid one fails before anything is read. a must not be
// nil and dir must not be empty; dir must exist, and nothing creates it. The format comes from the
// Config's AssetTemplate and AssetExt, never from the archive's bytes: a raw asset is the binary
// itself. A format with no extractor is an [*UnsupportedFormatError]. Inside an archive the binary
// is the file named BinaryName, followed by ".exe" when GOOS is "windows". In every format the
// binary must not be empty ([ErrEmptyAsset]).
//
// The staged binary is a file named TempPrefix followed by a random suffix, with mode 0700 whatever
// mode the archive records, synced to disk before Extract returns. At most MaxExtractBytes are
// written, or the extraction fails with an [*ExtractLimitError]. ctx is checked before each entry
// and between reads, and a cancelled ctx is an error matching [context.Canceled]. On any failure
// the partial file is already removed, and an error removing it is joined onto the error returned.
func (c Config) Extract(ctx context.Context, a *Archive, dir string) (*Staged, error) {
	if a == nil {
		return nil, errNoArchive
	}

	s, err := c.extract(ctx, a, dir)
	closeErr := a.Close()

	switch {
	case err != nil:
		return nil, errors.Join(err, closeErr)
	case closeErr != nil:
		return nil, errors.Join(closeErr, s.Discard())
	default:
		return s, nil
	}
}

// extract is [Config.Extract] without closing a.
func (c Config) extract(ctx context.Context, a *Archive, dir string) (*Staged, error) {
	r, err := c.resolve()
	if err != nil {
		return nil, err
	}

	if dir == "" {
		return nil, errNoStagingDir
	}

	extract := extractorFor(r.format())
	if extract == nil {
		return nil, &UnsupportedFormatError{Ext: r.AssetExt}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := os.CreateTemp(dir, r.TempPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("update: creating staged binary: %w", err)
	}

	s := &Staged{f: f, remove: os.Remove}

	err = extract(ctx, a, a.Size(), binaryFileName(r), r.MaxExtractBytes, f)
	if err == nil {
		err = wrapNonNil("update: setting staged binary mode", f.Chmod(stagedMode))
	}

	if err == nil {
		err = wrapNonNil("update: syncing staged binary", f.Sync())
	}

	if err != nil {
		return nil, errors.Join(err, s.Discard())
	}

	return s, nil
}

// extractFunc writes the binary held by the archive of size bytes that src reads to dst. name is
// the binary's file name, as [binaryFileName] returns it, and limit the most bytes it may write:
// past it, it returns an [*ExtractLimitError]. It checks ctx before each entry and reads through a
// [ctxReader], so a cancelled ctx stops it with an error matching ctx.Err().
type extractFunc func(ctx context.Context, src io.ReaderAt, size int64, name string, limit int64, dst io.Writer) error

// extractorFor returns the extractFunc for format f, or nil for a format with no extractor, which
// [Config.Extract] reports as an [*UnsupportedFormatError].
func extractorFor(f assetFormat) extractFunc {
	switch f {
	case formatRaw:
		return extractRaw
	case formatTarGz:
		return extractTarGz
	case formatZip:
		return extractZip
	default:
		return nil
	}
}

// extractRaw is the extractFunc of a raw asset: the archive is the binary, so its bytes are copied
// unchanged. An empty asset is [ErrEmptyAsset].
func extractRaw(ctx context.Context, src io.ReaderAt, size int64, name string, limit int64, dst io.Writer) error {
	if size == 0 {
		return fmt.Errorf("%w: %q", ErrEmptyAsset, name)
	}

	return copyBounded(dst, &ctxReader{ctx: ctx, r: io.NewSectionReader(src, 0, size)}, name, limit)
}

// copyBounded copies src to dst, writing at most limit bytes. A src holding more is an
// [*ExtractLimitError] for name, seen by reading one byte past the limit as [readBound] allows. Any
// other error reading or writing is returned wrapped with name.
func copyBounded(dst io.Writer, src io.Reader, name string, limit int64) error {
	n, err := io.Copy(dst, io.LimitReader(src, readBound(limit)))
	if err != nil {
		return fmt.Errorf("update: extracting %q: %w", name, err)
	}

	if n > limit {
		return &ExtractLimitError{Name: name, Limit: limit}
	}

	return nil
}

// ctxReader reads from r while ctx is live. Each Read first checks ctx and returns ctx.Err()
// without reading once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements [io.Reader].
func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p)
}

// binaryFileName is the file name of the binary a resolved Config names inside an archive:
// BinaryName, followed by ".exe" when GOOS is "windows".
func binaryFileName(r Config) string {
	if r.GOOS == "windows" {
		return r.BinaryName + ".exe"
	}

	return r.BinaryName
}

// unsafeEntryName reports whether an archive entry's name could reach outside the directory it
// is read into, or is not a portable relative path, and if so why. It refuses an empty name, a NUL
// byte, a backslash, an absolute path, a drive-letter path such as "C:\x" or "C:x", and a ".."
// component. A "." component, as in "./tool", is accepted.
func unsafeEntryName(name string) (reason string, unsafe bool) {
	switch {
	case name == "":
		return "empty name", true
	case strings.ContainsRune(name, 0):
		return "contains a NUL byte", true
	case strings.ContainsRune(name, '\\'):
		return "contains a backslash", true
	case strings.HasPrefix(name, "/"):
		return "absolute path", true
	case hasDriveLetter(name):
		return "drive-letter path", true
	}

	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return `contains a ".." component`, true
		}
	}

	return "", false
}

// hasDriveLetter reports whether name starts with a Windows drive letter and a colon.
func hasDriveLetter(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}

	l := name[0] | 0x20 // ASCII lower case

	return l >= 'a' && l <= 'z'
}
