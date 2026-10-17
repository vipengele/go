package update

import (
	"context"
	"errors"
	"fmt"
	"net"
	"unicode/utf8"
)

// maxErrorBody is the most response-body bytes a [StatusError] keeps.
const maxErrorBody = 1 << 10

// ErrPrerelease reports that the latest release is a pre-release and the source was not asked to
// include pre-releases.
var ErrPrerelease = errors.New("update: latest release is a pre-release")

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
