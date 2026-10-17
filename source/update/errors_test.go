package update

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// timeoutNetError is a net.Error with a configurable Timeout.
type timeoutNetError struct{ timeout bool }

func (e timeoutNetError) Error() string   { return "net error" }
func (e timeoutNetError) Timeout() bool   { return e.timeout }
func (e timeoutNetError) Temporary() bool { return false }

var _ net.Error = timeoutNetError{}

func TestNewStatusError_Body(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		wantBody string
	}{
		{name: "empty", body: "", wantBody: ""},
		{name: "short", body: "not found", wantBody: "not found"},
		{name: "exactly the limit", body: strings.Repeat("a", 1024), wantBody: strings.Repeat("a", 1024)},
		{name: "over the limit", body: strings.Repeat("a", 5000), wantBody: strings.Repeat("a", 1024)},
		{
			name:     "cut on a character boundary",
			body:     strings.Repeat("a", 1023) + "é" + "tail",
			wantBody: strings.Repeat("a", 1023),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newStatusError("https://example.test/x", 502, tc.body)

			if got.Body != tc.wantBody {
				t.Errorf("Body length = %d, want %d", len(got.Body), len(tc.wantBody))
			}

			if got.URL != "https://example.test/x" || got.StatusCode != 502 {
				t.Errorf("got URL %q status %d", got.URL, got.StatusCode)
			}
		})
	}
}

func TestStatusError_As(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("lookup: %w", newStatusError("https://example.test/x", 404, "gone"))

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatal("errors.As did not find *StatusError")
	}

	if statusErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", statusErr.StatusCode)
	}

	for _, want := range []string{"https://example.test/x", "404", "gone"} {
		if !strings.Contains(statusErr.Error(), want) {
			t.Errorf("Error() = %q, missing %q", statusErr.Error(), want)
		}
	}
}

func TestTagError(t *testing.T) {
	t.Parallel()

	cause := errors.New("not a release version")
	err := fmt.Errorf("lookup: %w", &TagError{Tag: "source/update/v1.2.3", Err: cause})

	var tagErr *TagError
	if !errors.As(err, &tagErr) {
		t.Fatal("errors.As did not find *TagError")
	}

	if tagErr.Tag != "source/update/v1.2.3" {
		t.Errorf("Tag = %q", tagErr.Tag)
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is does not reach the cause")
	}

	if !strings.Contains(tagErr.Error(), "source/update/v1.2.3") {
		t.Errorf("Error() = %q, missing the tag", tagErr.Error())
	}

	if got := (&TagError{Tag: "x"}).Error(); !strings.Contains(got, `"x"`) {
		t.Errorf("Error() without a cause = %q", got)
	}
}

func TestErrPrerelease_Is(t *testing.T) {
	t.Parallel()

	if !errors.Is(fmt.Errorf("lookup: %w", ErrPrerelease), ErrPrerelease) {
		t.Error("errors.Is does not match ErrPrerelease through a wrap")
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	const url = "https://example.test/x"

	alreadyTimeout := &TimeoutError{URL: "https://other.test", Err: context.DeadlineExceeded}
	other := errors.New("connection refused")

	tests := []struct {
		name        string
		err         error
		wantTimeout bool
		wantSame    bool
		wantIs      error
	}{
		{name: "nil", err: nil, wantSame: true},
		{name: "deadline exceeded", err: context.DeadlineExceeded, wantTimeout: true, wantIs: context.DeadlineExceeded},
		{
			name:        "wrapped deadline exceeded",
			err:         fmt.Errorf("do: %w", context.DeadlineExceeded),
			wantTimeout: true,
			wantIs:      context.DeadlineExceeded,
		},
		{name: "net error reporting timeout", err: timeoutNetError{timeout: true}, wantTimeout: true},
		{name: "net error not reporting timeout", err: timeoutNetError{timeout: false}, wantSame: true},
		{name: "canceled", err: context.Canceled, wantSame: true},
		{name: "wrapped canceled", err: fmt.Errorf("do: %w", context.Canceled), wantSame: true},
		{name: "already a timeout", err: alreadyTimeout, wantSame: true},
		{name: "other error", err: other, wantSame: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyError(url, tc.err)

			var timeoutErr *TimeoutError

			isTimeout := errors.As(got, &timeoutErr)
			if tc.wantTimeout != (isTimeout && timeoutErr.URL == url) && tc.err != alreadyTimeout {
				t.Fatalf("TimeoutError = %v (%v), want %v", isTimeout, got, tc.wantTimeout)
			}

			if tc.wantSame && got != tc.err {
				t.Errorf("got %v, want the error unchanged", got)
			}

			if tc.wantTimeout {
				if !timeoutErr.Timeout() {
					t.Error("Timeout() = false")
				}

				var netErr net.Error
				if !errors.As(got, &netErr) || !netErr.Timeout() {
					t.Error("result is not a net.Error reporting Timeout")
				}

				if !errors.Is(got, tc.err) {
					t.Error("errors.Is does not reach the cause")
				}
			}

			if tc.wantIs != nil && !errors.Is(got, tc.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", got, tc.wantIs)
			}

			if errors.Is(tc.err, context.Canceled) && isTimeout {
				t.Error("a cancellation was wrapped as a TimeoutError")
			}
		})
	}
}
