package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"

	"go.vipengele.org/update/internal/minisign"
)

// errNoURL reports a [Selection] asset with an empty URL, which [Config.Download] cannot request.
var errNoURL = errors.New("update: asset has no URL")

// maxSignatureBytes is the most bytes of a signature asset [Config.Download] reads: a few times the
// 300-odd bytes minisign writes, leaving room for a long trusted comment.
const maxSignatureBytes = 4 << 10

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
// digest matches the one sel's checksums file lists for the archive's name, and, unless
// AllowUnsigned is set, once the checksums file's minisign signature verifies with one of the
// TrustedKeys.
//
// The Config is resolved first, so an invalid one fails before any request is made, and an empty
// URL on the archive, the checksums file or, unless AllowUnsigned is set, the signature fails
// before any request too. The checksums file is read in full first. Unless AllowUnsigned is set,
// the signature is fetched next and checked against the checksums file's bytes with the trusted
// key whose key id it carries; a signature that does not parse is a [*SignatureMalformedError], one
// whose key id no trusted key carries an [*UnknownKeyError], and one that does not verify a
// [*SignatureMismatchError]. Only a verified checksums file is parsed, and only then is the
// archive requested, so a failed signature, and a checksums file with no entry for the archive or
// with two different ones, a [*ChecksumNotFoundError] or a [*ChecksumConflictError], fail without
// the archive being downloaded. The archive is then written to a file named TempPrefix followed by
// a random suffix in TempDir, which must exist, with mode 0600, and hashed as it is written; a
// digest other than the listed one is a [*ChecksumMismatchError]. The returned [Archive] is
// positioned at offset 0, and its Close removes the file. On any failure every temporary file is
// already removed, and an error removing one is joined onto the error returned.
//
// Each request goes through the resolved HTTPClient, follows redirects and is bounded by
// DownloadTimeout, a deadline covering its headers and its body; a shorter deadline on ctx wins.
// HTTPS is not enforced: the signature is what authenticates the checksums file, and the digest
// check carries that to the archive. Under AllowUnsigned no signature is fetched, the checksums
// file is trusted as it arrives, and the digest check vouches only for the archive's integrity.
// The archive may carry at most MaxDownloadBytes, the checksums file at most 1 MiB and the
// signature at most 4 KiB, or the download fails with a [*SizeLimitError]: up front when the
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

	assets := []Asset{sel.Archive, sel.Checksums}
	if !r.AllowUnsigned {
		// A Selection built for an unsigned release has a zero Signature, which the error names by
		// the asset Select would have named.
		sig := sel.Signature
		if sig.Name == "" {
			sig.Name = sel.Checksums.Name + signatureSuffix
		}
		assets = append(assets, sig)
	}

	for _, a := range assets {
		if a.URL == "" {
			return nil, fmt.Errorf("%w: %q", errNoURL, a.Name)
		}
	}

	sums, err := r.fetchBounded(ctx, sel, sel.Checksums, maxChecksumsBytes, "checksums")
	if err != nil {
		return nil, err
	}

	if !r.AllowUnsigned {
		if err := r.verifySignature(ctx, sel, sums); err != nil {
			return nil, err
		}
	}

	want, err := parseChecksum(bytes.NewReader(sums), sel.Archive.Name)
	if err != nil {
		return nil, err
	}

	return r.fetchArchive(ctx, sel, want, o.progress)
}

// fetchBounded downloads asset in full and returns its bytes, failing with a [*SizeLimitError]
// when it carries more than limit. what names the asset in the error a failed read gives.
func (r Config) fetchBounded(ctx context.Context, sel Selection, asset Asset, limit int64, what string) ([]byte, error) {
	var data []byte

	err := r.get(ctx, sel, asset, func(resp *http.Response) error {
		if expectedSize(resp, asset) > limit {
			return &SizeLimitError{URL: asset.URL, Limit: limit}
		}

		b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return fmt.Errorf("update: GET %s: reading %s: %w", asset.URL, what, err)
		}

		if int64(len(b)) > limit {
			return &SizeLimitError{URL: asset.URL, Limit: limit}
		}

		data = b

		return nil
	})
	if err != nil {
		return nil, err
	}

	return data, nil
}

// verifySignature downloads sel's signature and verifies it over sums, the checksums file's bytes,
// with the resolved TrustedKeys. It returns the error [Config.Download] documents for a signature
// that does not parse, names an untrusted key or does not verify.
func (r Config) verifySignature(ctx context.Context, sel Selection, sums []byte) error {
	sig, err := r.fetchBounded(ctx, sel, sel.Signature, maxSignatureBytes, "signature")
	if err != nil {
		return err
	}

	keys := make([]minisign.PublicKey, 0, len(r.TrustedKeys))
	for _, entry := range r.TrustedKeys {
		// resolve has validated every entry, so none fails to parse here; one that did would only
		// leave its key untrusted.
		if key, err := minisign.ParsePublicKey(entry); err == nil {
			keys = append(keys, key)
		}
	}

	_, err = minisign.Verify(keys, sums, sig)

	return signatureError(sel.Signature.Name, err)
}

// signatureError turns an error of [minisign.Verify] for the signature asset name into the error a
// caller sees. A nil error is returned as nil.
func signatureError(name string, err error) error {
	var (
		malformed *minisign.MalformedSignatureError
		unknown   *minisign.UnknownKeyError
		bad       *minisign.BadSignatureError
	)

	switch {
	case err == nil:
		return nil
	case errors.As(err, &malformed):
		return &SignatureMalformedError{Name: name, Err: err}
	case errors.As(err, &unknown):
		return &UnknownKeyError{Name: name, KeyID: unknown.ID.String()}
	case errors.As(err, &bad):
		return &SignatureMismatchError{Name: name, KeyID: bad.ID.String(), Err: err}
	default:
		return fmt.Errorf("update: verifying signature %q: %w", name, err)
	}
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

		written, err = io.Copy(io.MultiWriter(a.f, h, pw), io.LimitReader(resp.Body, readBound(r.MaxDownloadBytes)))
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

	if err == nil {
		err = a.finish()
	}

	if err != nil {
		if a != nil {
			return nil, a.discard(err)
		}

		return nil, err
	}

	if progress != nil {
		progress(written, written)
	}

	return a, nil
}

// readBound is how many bytes to read under a cap of limit so that a body over the cap is seen: one
// more than limit, saturating at [math.MaxInt64], where a body cannot exceed the cap and the extra
// byte would overflow to a negative bound that reads nothing.
func readBound(limit int64) int64 {
	if limit == math.MaxInt64 {
		return limit
	}

	return limit + 1
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
