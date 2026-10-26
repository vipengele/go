package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
)

// gzipMagic is the two bytes every gzip stream starts with.
const gzipMagic = "\x1f\x8b"

// extractTarGz is the extractFunc of a tar.gz archive. The binary is the one regular file whose
// base name is name, at any depth; a directory of that name is not it. No match is a
// [*BinaryNotFoundError], more than one a [*MultipleBinariesError], and a match of zero bytes is
// [ErrEmptyAsset].
//
// Every entry is checked before anything is written to dst, and any one entry the policy of
// [tarEntryKind] refuses rejects the whole archive with an [*UnsafeEntryError], wherever it sits
// relative to the binary. The archive is therefore read twice: the first pass checks every entry
// and reads the gzip stream to its trailer, the second copies the binary's body to dst.
//
// limit bounds the whole decompressed stream of each pass, tar headers and the bodies of entries
// that are skipped included, so a compression bomb in any entry is an [*ExtractLimitError].
// Bytes that do not start with the gzip magic are [ErrNotGzip], and a stream that ends early is
// [ErrTruncatedGzip].
func extractTarGz(ctx context.Context, src io.ReaderAt, size int64, name string, limit int64, dst io.Writer) error {
	if err := checkGzipMagic(src, size); err != nil {
		return err
	}

	index, err := scanTarGz(ctx, src, size, name, limit)
	if err != nil {
		return err
	}

	return copyTarGzEntry(ctx, src, size, index, limit, dst)
}

// checkGzipMagic returns [ErrNotGzip] unless the size bytes src reads start with the gzip magic.
// [gzip.NewReader] cannot tell these apart from a truncated header: it reads ten bytes before it
// looks at the magic.
func checkGzipMagic(src io.ReaderAt, size int64) error {
	if size < int64(len(gzipMagic)) {
		return fmt.Errorf("%w: %d bytes", ErrNotGzip, size)
	}

	head := make([]byte, len(gzipMagic))
	if _, err := src.ReadAt(head, 0); err != nil {
		return fmt.Errorf("update: reading archive: %w", err)
	}

	if string(head) != gzipMagic {
		return fmt.Errorf("%w: starts with %#x", ErrNotGzip, head)
	}

	return nil
}

// scanTarGz reads every entry of the archive, checking each against the entry policy, and the
// gzip stream past the end of the tar to its trailer. It returns the index, in the order
// [tar.Reader.Next] returns entries, of the one regular file named name.
func scanTarGz(ctx context.Context, src io.ReaderAt, size int64, name string, limit int64) (int, error) {
	s, err := openTarGz(ctx, src, size, limit)
	if err != nil {
		return 0, err
	}

	var (
		matches []string
		index   int
		empty   bool
	)

	for i := 0; ; i++ {
		hdr, err := s.next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return 0, err
		}

		kind, err := tarEntryKind(hdr)
		if err != nil {
			return 0, err
		}

		if kind == tarEntryFile && path.Base(hdr.Name) == name {
			matches = append(matches, hdr.Name)
			index = i
			empty = hdr.Size == 0
		}
	}

	if err := s.drain(); err != nil {
		return 0, err
	}

	switch {
	case len(matches) == 0:
		return 0, &BinaryNotFoundError{Name: name}
	case len(matches) > 1:
		return 0, &MultipleBinariesError{Name: name, Entries: matches}
	case empty:
		return 0, fmt.Errorf("%w: archive entry %q", ErrEmptyAsset, matches[0])
	}

	return index, nil
}

// copyTarGzEntry copies the body of the entry at index, which [scanTarGz] found, to dst.
func copyTarGzEntry(ctx context.Context, src io.ReaderAt, size int64, index int, limit int64, dst io.Writer) error {
	s, err := openTarGz(ctx, src, size, limit)
	if err != nil {
		return err
	}

	var hdr *tar.Header
	for range index + 1 {
		if hdr, err = s.next(ctx); err != nil {
			return err
		}
	}

	return tarGzError(copyBounded(dst, s.tr, hdr.Name, limit))
}

// tarGzStream is one pass over a tar.gz archive: a tar reader over the gzip stream, decompressed
// through a [budgetReader] and a [ctxReader].
type tarGzStream struct {
	budget *budgetReader
	tr     *tar.Reader
}

// openTarGz starts a pass over the size bytes src reads, whose decompressed stream may hold at
// most limit bytes.
func openTarGz(ctx context.Context, src io.ReaderAt, size int64, limit int64) (*tarGzStream, error) {
	zr, err := gzip.NewReader(io.NewSectionReader(src, 0, size))
	if err != nil {
		return nil, tarGzError(fmt.Errorf("update: reading gzip header: %w", err))
	}

	budget := &budgetReader{r: &ctxReader{ctx: ctx, r: zr}, limit: limit}

	return &tarGzStream{budget: budget, tr: tar.NewReader(budget)}, nil
}

// next checks ctx and returns the next entry's header, or io.EOF at the end of the tar. The
// entry's name becomes the one a later [*ExtractLimitError] reports.
func (s *tarGzStream) next(ctx context.Context) (*tar.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hdr, err := s.tr.Next()
	if errors.Is(err, tar.ErrInsecurePath) && hdr != nil {
		return nil, &UnsafeEntryError{Name: hdr.Name, Reason: "not a local path", Err: err}
	}

	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	}

	if err != nil {
		return nil, tarGzError(fmt.Errorf("update: reading tar header: %w", err))
	}

	s.budget.name = hdr.Name

	return hdr, nil
}

// drain reads the decompressed stream past the end of the tar to the gzip trailer, so that a
// stream cut short or corrupt after the tar's last entry is an error, still within the budget.
func (s *tarGzStream) drain() error {
	if _, err := io.Copy(io.Discard, s.budget); err != nil {
		return tarGzError(fmt.Errorf("update: reading gzip stream: %w", err))
	}

	return nil
}

// tarGzError returns err as the caller sees it: an [*ExtractLimitError] anywhere in its chain
// unwrapped, so that wrapping by the tar reader does not hide it, and an unexpected end of the
// stream as [ErrTruncatedGzip]. Any other error, including nil, is returned unchanged.
func tarGzError(err error) error {
	var limitErr *ExtractLimitError
	if errors.As(err, &limitErr) {
		return limitErr
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", ErrTruncatedGzip, err)
	}

	return err
}

// tarKind is what the entry policy makes of a tar entry it accepts.
type tarKind int

const (
	// tarEntryFile is a regular file: the binary, when its base name is the binary's.
	tarEntryFile tarKind = iota + 1
	// tarEntrySkipped is an entry that is accepted and never extracted.
	tarEntrySkipped
)

// tarEntryKind applies the entry policy to hdr, returning an [*UnsafeEntryError] for an entry it
// refuses. Accepted are:
//
//   - a regular file, [tar.TypeReg]; the reader reports the legacy [tar.TypeRegA] as TypeReg, or
//     as TypeDir when the name ends in a slash;
//   - a directory, [tar.TypeDir], skipped;
//   - a global PAX header, [tar.TypeXGlobalHeader], skipped. Its name is not a path in the
//     archive, and writers commonly give it an absolute one, so it is not checked.
//
// Every other type is refused: a hard link, [tar.TypeLink], and a symbolic link,
// [tar.TypeSymlink], whatever their target, because a link can stand for the binary or reach
// outside the archive; a character or block device, a FIFO and a contiguous file
// ([tar.TypeChar], [tar.TypeBlock], [tar.TypeFifo], [tar.TypeCont]); a GNU sparse file
// ([tar.TypeGNUSparse]); and any type the reader does not know. Per-file PAX headers and GNU long
// names and link names ([tar.TypeXHeader], [tar.TypeGNULongName], [tar.TypeGNULongLink]) are
// merged by the reader into the entry they describe and never reach the policy. The name of an
// accepted entry must pass [unsafeEntryName].
func tarEntryKind(hdr *tar.Header) (tarKind, error) {
	var kind tarKind

	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader:
		return tarEntrySkipped, nil
	case tar.TypeReg:
		kind = tarEntryFile
	case tar.TypeDir:
		kind = tarEntrySkipped
	case tar.TypeLink:
		return 0, &UnsafeEntryError{Name: hdr.Name, Reason: "hard link"}
	case tar.TypeSymlink:
		return 0, &UnsafeEntryError{Name: hdr.Name, Reason: "symbolic link"}
	default:
		return 0, &UnsafeEntryError{
			Name:   hdr.Name,
			Reason: fmt.Sprintf("type %q is not a regular file or a directory", hdr.Typeflag),
		}
	}

	if reason, unsafe := unsafeEntryName(hdr.Name); unsafe {
		return 0, &UnsafeEntryError{Name: hdr.Name, Reason: reason}
	}

	return kind, nil
}

// budgetReader reads at most limit bytes from r in total. Reading past them is an
// [*ExtractLimitError] for name, the entry whose header was read last, or "" before the first; it
// is seen by reading one byte past the limit, as [readBound] allows, and that byte is withheld.
type budgetReader struct {
	r     io.Reader
	limit int64
	read  int64
	name  string
}

// Read implements [io.Reader].
func (b *budgetReader) Read(p []byte) (int, error) {
	if b.read > b.limit {
		return 0, b.exceeded()
	}

	if rest := readBound(b.limit) - b.read; int64(len(p)) > rest {
		p = p[:rest]
	}

	n, err := b.r.Read(p)
	b.read += int64(n)

	if b.read > b.limit {
		return n - 1, b.exceeded()
	}

	return n, err
}

// exceeded is the error reading past the limit returns.
func (b *budgetReader) exceeded() error {
	return &ExtractLimitError{Name: b.name, Limit: b.limit}
}
