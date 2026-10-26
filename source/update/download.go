package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// errNoURL reports a [Selection] asset with an empty URL, which [Config.Download] cannot request.
var errNoURL = errors.New("update: asset has no URL")

// DownloadOption configures one call to [Config.Download].
type DownloadOption func(*downloadOptions)

// downloadOptions is what the [DownloadOption]s of one call to [Config.Download] set.
type downloadOptions struct {
	progress func(done, total int64)
}

// WithProgress has [Config.Download] report the archive's progress to fn, synchronously on the
// goroutine that called Download. done is the number of archive bytes written so far and never
// decreases. total is the response's Content-Length when it has one, else the archive's non-zero
// [Asset.Size], else -1. On success fn is called a last time with done and total both equal to the
// number of bytes written, after the archive is verified. After a failure fn is not called again,
// and the checksums file is never reported.
func WithProgress(fn func(done, total int64)) DownloadOption {
	return func(o *downloadOptions) {
		o.progress = fn
	}
}

// Download fetches the archive sel names to a temporary file and returns it once its SHA-256
// digest matches the one sel's checksums file lists for the archive's name.
//
// The Config is resolved first, so an invalid one fails before any request is made, and an empty
// URL on either asset fails before any request too. The checksums file is fetched and parsed
// before the archive is requested, so a file with no entry for the archive, or with two different
// ones, fails without the archive being downloaded: a [*ChecksumNotFoundError] or a
// [*ChecksumConflictError]. The archive is then written to a file named TempPrefix followed by a
// random suffix in TempDir, which must exist, with mode 0600, and hashed as it is written; a digest
// other than the listed one is a [*ChecksumMismatchError]. The returned [Archive] is positioned at
// offset 0, and its Close removes the file. On any failure every temporary file is already
// removed, and an error removing one is joined onto the error returned.
//
// Each of the two requests goes through the resolved HTTPClient, follows redirects and is bounded
// by DownloadTimeout, a deadline covering its headers and its body; a shorter deadline on ctx wins.
// HTTPS is not enforced: the digest check is what the archive's integrity rests on, and the
// checksums file is trusted as it arrives. The archive may carry at most MaxDownloadBytes and the
// checksums file at most 1 MiB, or the download fails with a [*SizeLimitError]: up front when the
// response's Content-Length, or else the asset's non-zero Size, is above the limit, and while
// reading otherwise. The Content-Length and Size are otherwise hints: a body of another length is
// not an error, and the digest decides.
//
// A response other than 200 OK is a [*StatusError], except a 404 when sel is Located, which is a
// [*AssetNotFoundError] for sel's Tag and the requested asset's name: the URL was constructed,
// and the asset is not there. An expired deadline is a [*TimeoutError], and a cancelled ctx an
// error matching [context.Canceled].
func (c Config) Download(ctx context.Context, sel Selection, opts ...DownloadOption) (*Archive, error) {
	r, err := c.resolve()
	if err != nil {
		return nil, err
	}

	var o downloadOptions
	for _, opt := range opts {
		opt(&o)
	}

	for _, a := range []Asset{sel.Archive, sel.Checksums} {
		if a.URL == "" {
			return nil, fmt.Errorf("%w: %q", errNoURL, a.Name)
		}
	}

	want, err := r.fetchChecksum(ctx, sel)
	if err != nil {
		return nil, err
	}

	return r.fetchArchive(ctx, sel, want, o.progress)
}

// fetchChecksum downloads sel's checksums file and returns the digest it lists for the archive.
func (r Config) fetchChecksum(ctx context.Context, sel Selection) ([]byte, error) {
	rawURL := sel.Checksums.URL

	var digest []byte

	err := r.get(ctx, sel, sel.Checksums, func(resp *http.Response) error {
		if expectedSize(resp, sel.Checksums) > maxChecksumsBytes {
			return &SizeLimitError{URL: rawURL, Limit: maxChecksumsBytes}
		}

		d, err := parseChecksum(resp.Body, sel.Archive.Name)

		var sizeErr *SizeLimitError

		var notFound *ChecksumNotFoundError

		var conflict *ChecksumConflictError

		switch {
		case err == nil:
			digest = d
			return nil
		case errors.As(err, &sizeErr):
			return &SizeLimitError{URL: rawURL, Limit: sizeErr.Limit}
		case errors.As(err, &notFound), errors.As(err, &conflict):
			return err
		default:
			return fmt.Errorf("update: GET %s: reading checksums: %w", rawURL, err)
		}
	})
	if err != nil {
		return nil, err
	}

	return digest, nil
}

// fetchArchive downloads sel's archive to a temporary file, checks its digest against want and
// returns it rewound. progress, when not nil, is reported to as [WithProgress] describes.
func (r Config) fetchArchive(ctx context.Context, sel Selection, want []byte, progress func(done, total int64)) (*Archive, error) {
	rawURL := sel.Archive.URL

	var (
		a       *Archive
		written int64
		got     []byte
	)

	err := r.get(ctx, sel, sel.Archive, func(resp *http.Response) error {
		total := expectedSize(resp, sel.Archive)
		if total > r.MaxDownloadBytes {
			return &SizeLimitError{URL: rawURL, Limit: r.MaxDownloadBytes}
		}

		var err error
		if a, err = createArchive(r.TempDir, r.TempPrefix); err != nil {
			return err
		}

		h := sha256.New()
		pw := &progressWriter{total: total, report: progress}

		written, err = io.Copy(io.MultiWriter(a.f, h, pw), io.LimitReader(resp.Body, r.MaxDownloadBytes+1))
		if err != nil {
			return fmt.Errorf("update: GET %s: %w", rawURL, err)
		}

		if written > r.MaxDownloadBytes {
			return &SizeLimitError{URL: rawURL, Limit: r.MaxDownloadBytes}
		}

		got = h.Sum(nil)

		return nil
	})
	if err == nil && !bytes.Equal(got, want) {
		err = &ChecksumMismatchError{Name: sel.Archive.Name, Want: hex.EncodeToString(want), Got: hex.EncodeToString(got)}
	}

	if err != nil {
		if a != nil {
			return nil, a.discard(err)
		}

		return nil, err
	}

	if a, err = a.finish(); err != nil {
		return nil, err // [lydite:exclude_from_mutation][finish fails only on a file the download has just written through, which no test can make unreadable]
	}

	if progress != nil {
		progress(written, written)
	}

	return a, nil
}

// get makes one GET request for asset through the resolved HTTPClient, following redirects and
// bounded by DownloadTimeout, and hands a 200 OK response to read. Any other status is the error
// [Config.Download] documents for it, and read is not called. The body is drained and closed
// after read returns, and every error is passed through [classifyError].
func (r Config) get(ctx context.Context, sel Selection, asset Asset, read func(*http.Response) error) error {
	ctx, cancel := context.WithTimeout(ctx, r.DownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return fmt.Errorf("update: GET %s: %w", asset.URL, err)
	}

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return classifyError(asset.URL, err)
	}
	defer drainAndClose(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		return classifyError(asset.URL, read(resp))
	case resp.StatusCode == http.StatusNotFound && sel.Located:
		return &AssetNotFoundError{Tag: sel.Tag, Name: asset.Name}
	default:
		return statusError(asset.URL, resp)
	}
}

// expectedSize returns the length resp announces for asset: its Content-Length when it has one,
// else asset's non-zero Size, else -1.
func expectedSize(resp *http.Response, asset Asset) int64 {
	if resp.ContentLength >= 0 {
		return resp.ContentLength
	}

	if asset.Size > 0 {
		return asset.Size
	}

	return -1
}

// progressWriter counts the bytes written through it and reports the running count and total to
// report after each write. A nil report only counts.
type progressWriter struct {
	done   int64
	total  int64
	report func(done, total int64)
}

// Write implements [io.Writer]. It never fails.
func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.report != nil {
		p.report(p.done, p.total)
	}

	return len(b), nil
}
