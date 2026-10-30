package update

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"unicode/utf8"
)

// maxErrorBody is the most response-body bytes a [StatusError] keeps.
const maxErrorBody = 1 << 10

// ErrPrerelease reports that the latest release is a pre-release and the source was not asked to
// include pre-releases.
var ErrPrerelease = errors.New("update: latest release is a pre-release")

// ErrAssetsNotListed reports a release whose assets the source did not list, from a [Source] that
// cannot say where an asset of a given name is downloaded from: it is not an [AssetLocator].
var ErrAssetsNotListed = errors.New("update: release assets are not listed")

// StatusError reports a response with a non-success HTTP status.
type StatusError struct {
	// URL is the address that was requested.
	URL string
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Body is the start of the response body, at most 1 KiB.
	Body string
}

// newStatusError returns a [StatusError] whose Body is body cut to 1 KiB, on a character
// boundary so the result stays valid UTF-8 when body is.
func newStatusError(url string, statusCode int, body string) *StatusError {
	if len(body) > maxErrorBody {
		cut := maxErrorBody
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}

		body = body[:cut]
	}

	return &StatusError{URL: url, StatusCode: statusCode, Body: body}
}

// Error implements error.
func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("update: GET %s: unexpected status %d", e.URL, e.StatusCode)
	}

	return fmt.Sprintf("update: GET %s: unexpected status %d: %s", e.URL, e.StatusCode, e.Body)
}

// TimeoutError reports a request whose deadline expired: the source's own timeout, a shorter
// deadline on the caller's context, or the HTTP client's timeout.
type TimeoutError struct {
	// URL is the address that was requested.
	URL string
	// Err is the underlying cause.
	Err error
}

// Error implements error.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("update: GET %s: timed out: %v", e.URL, e.Err)
}

// Unwrap returns the underlying cause, so errors.Is reaches [context.DeadlineExceeded] when the
// cause is a context deadline.
func (e *TimeoutError) Unwrap() error { return e.Err }

// Timeout reports true, satisfying [net.Error].
func (*TimeoutError) Timeout() bool { return true }

// Temporary reports true, satisfying [net.Error]: a timed-out request may succeed when retried.
func (*TimeoutError) Temporary() bool { return true }

var _ net.Error = (*TimeoutError)(nil)

// TagError reports a release tag that is not a plain release version, such as a malformed tag
// or a prefixed one like "source/update/v1.2.3".
type TagError struct {
	// Tag is the tag as the source published it.
	Tag string
	// Err is why the tag was rejected.
	Err error
}

// Error implements error.
func (e *TagError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("update: invalid release tag %q", e.Tag)
	}

	return fmt.Sprintf("update: invalid release tag %q: %v", e.Tag, e.Err)
}

// Unwrap returns the reason the tag was rejected.
func (e *TagError) Unwrap() error { return e.Err }

// AssetNotFoundError reports a release that does not list the asset a [Config] names for the
// platform.
type AssetNotFoundError struct {
	// Tag is the release's tag.
	Tag string
	// Name is the asset name that was expected.
	Name string
}

// Error implements error.
func (e *AssetNotFoundError) Error() string {
	return fmt.Sprintf("update: release %s has no asset %q", e.Tag, e.Name)
}

// ChecksumMismatchError reports a downloaded archive whose SHA-256 digest differs from the one the
// checksums file lists for it.
type ChecksumMismatchError struct {
	// Name is the archive's asset name.
	Name string
	// Want is the hex digest the checksums file lists.
	Want string
	// Got is the hex digest of the downloaded bytes.
	Got string
}

// Error implements error.
func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("update: checksum mismatch for %q: want %s, got %s", e.Name, e.Want, e.Got)
}

// ChecksumNotFoundError reports a checksums file with no entry for the archive.
type ChecksumNotFoundError struct {
	// Name is the archive's asset name.
	Name string
}

// Error implements error.
func (e *ChecksumNotFoundError) Error() string {
	return fmt.Sprintf("update: checksums file has no entry for %q", e.Name)
}

// ChecksumConflictError reports a checksums file that lists two different digests for the archive.
type ChecksumConflictError struct {
	// Name is the archive's asset name.
	Name string
}

// Error implements error.
func (e *ChecksumConflictError) Error() string {
	return fmt.Sprintf("update: checksums file lists conflicting digests for %q", e.Name)
}

// SizeLimitError reports a download that exceeded its byte cap, whether the archive or the
// checksums file.
type SizeLimitError struct {
	// URL is the address that was downloaded.
	URL string
	// Limit is the most bytes the download may carry.
	Limit int64
}

// Error implements error.
func (e *SizeLimitError) Error() string {
	return fmt.Sprintf("update: GET %s: exceeds the limit of %d bytes", e.URL, e.Limit)
}

// ErrEmptyAsset reports a binary of zero bytes: a raw asset, or the entry an archive holds.
var ErrEmptyAsset = errors.New("update: binary is empty")

// ErrNotGzip reports a tar.gz archive whose bytes do not start with the gzip magic number.
var ErrNotGzip = errors.New("update: archive is not gzip-compressed")

// ErrTruncatedGzip reports a tar.gz archive cut short: a gzip stream that ends before its trailer,
// or a tar stream that ends early inside an intact gzip stream.
var ErrTruncatedGzip = errors.New("update: gzip stream is truncated")

// ExtractLimitError reports an extraction that exceeded its byte cap, [Config.MaxExtractBytes].
type ExtractLimitError struct {
	// Name is the entry the cap was reached in. For a tar.gz archive it is the entry whose header
	// was read last, empty when the cap is reached before the first header; for a zip archive, the
	// selected entry; for a raw asset, the binary's file name.
	Name string
	// Limit is the most bytes the extraction may read or write.
	Limit int64
}

// Error implements error.
func (e *ExtractLimitError) Error() string {
	return fmt.Sprintf("update: extracting %q: exceeds the limit of %d bytes", e.Name, e.Limit)
}

// UnsafeEntryError reports an archive entry that extraction refuses to read: a name that could
// reach outside the archive, such as an absolute path, a ".." component, a backslash, a drive
// letter or a NUL byte, or an entry that is not a regular file or a directory, such as a symbolic
// link, a hard link or a device.
type UnsafeEntryError struct {
	// Name is the entry's name as the archive records it.
	Name string
	// Reason says why the entry is refused.
	Reason string
	// Err is the underlying cause, when the archive reader reported one; nil otherwise.
	Err error
}

// Error implements error.
func (e *UnsafeEntryError) Error() string {
	return fmt.Sprintf("update: unsafe archive entry %q: %s", e.Name, e.Reason)
}

// Unwrap returns the underlying cause, so errors.Is reaches an error such as
// [archive/zip.ErrInsecurePath].
func (e *UnsafeEntryError) Unwrap() error { return e.Err }

// BinaryNotFoundError reports an archive that holds no regular file named like the binary.
type BinaryNotFoundError struct {
	// Name is the binary's file name that was looked for.
	Name string
}

// Error implements error.
func (e *BinaryNotFoundError) Error() string {
	return fmt.Sprintf("update: archive has no file named %q", e.Name)
}

// MultipleBinariesError reports an archive that holds more than one regular file named like the
// binary, so which one to stage is ambiguous.
type MultipleBinariesError struct {
	// Name is the binary's file name that was looked for.
	Name string
	// Entries are the names of the matching entries, in archive order.
	Entries []string
}

// Error implements error.
func (e *MultipleBinariesError) Error() string {
	return fmt.Sprintf("update: archive has %d files named %q: %q", len(e.Entries), e.Name, e.Entries)
}

// UnsupportedFormatError reports an archive format that [Config.Extract] has no extractor for.
type UnsupportedFormatError struct {
	// Ext is the AssetExt that names the format.
	Ext string
}

// Error implements error.
func (e *UnsupportedFormatError) Error() string {
	return fmt.Sprintf("update: no extractor for archive format %q", e.Ext)
}

// PermissionError reports a filesystem operation on the installed executable's path that the
// operating system refused for lack of permission.
type PermissionError struct {
	// Path is the file or directory the operation was refused on.
	Path string
	// Op names the operation that was refused, such as "create" or "rename".
	Op string
	// Hint is a sentence for the user on how to get write access, such as reinstalling into a
	// directory they own or rerunning with elevated privileges.
	Hint string
	// Err is the underlying cause.
	Err error
}

// Error implements error.
func (e *PermissionError) Error() string {
	msg := fmt.Sprintf("update: %s %s: permission denied", e.Op, e.Path)
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}

	if e.Hint != "" {
		msg = fmt.Sprintf("%s (%s)", msg, e.Hint)
	}

	return msg
}

// Unwrap returns the underlying cause.
func (e *PermissionError) Unwrap() error { return e.Err }

// Is reports whether target is [fs.ErrPermission], so errors.Is matches whatever the underlying
// cause is.
func (*PermissionError) Is(target error) bool { return target == fs.ErrPermission }

// BackupLockedError reports a backup of the previous binary that could not be removed because
// another process holds it open, as a running executable is on Windows. The backup stays on disk
// and the update itself has succeeded.
type BackupLockedError struct {
	// Path is the backup file.
	Path string
	// Err is the underlying cause.
	Err error
}

// Error implements error.
func (e *BackupLockedError) Error() string {
	return fmt.Sprintf("update: backup %q is in use and was not removed: %v", e.Path, e.Err)
}

// Unwrap returns the underlying cause.
func (e *BackupLockedError) Unwrap() error { return e.Err }

// RestoreError reports an install whose rename of the new binary into place failed and whose
// restore of the previous binary failed too, so the executable's path may hold no binary.
type RestoreError struct {
	// Path is the executable's path.
	Path string
	// Backup is the file holding the previous binary, to be moved back to Path by hand.
	Backup string
	// Err is why the new binary could not be moved into place.
	Err error
	// RestoreErr is why the previous binary could not be moved back.
	RestoreErr error
}

// Error implements error.
func (e *RestoreError) Error() string {
	return fmt.Sprintf(
		"update: installing %q failed: %v; restoring the previous binary from %q failed: %v",
		e.Path, e.Err, e.Backup, e.RestoreErr,
	)
}

// Unwrap returns both causes, so errors.Is and errors.As reach either.
func (e *RestoreError) Unwrap() []error { return []error{e.Err, e.RestoreErr} }

// classifyError turns an error from a request to url into the error a caller sees. A cancellation
// is returned as it is, because it is the caller's own doing and not a timeout. An expired context
// deadline or a [net.Error] reporting Timeout becomes a [*TimeoutError]. Any other error,
// including a nil one, is returned unchanged.
func classifyError(url string, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}

	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		return err
	}

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &TimeoutError{URL: url, Err: err}
	}

	return err
}
