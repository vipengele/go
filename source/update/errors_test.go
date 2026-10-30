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
		{name: "only continuation bytes", body: strings.Repeat("\x80", 5000), wantBody: ""},
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

func TestStatusError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *StatusError
		want string
	}{
		{
			name: "empty body",
			err:  &StatusError{URL: "https://example.test/x", StatusCode: 502},
			want: "update: GET https://example.test/x: unexpected status 502",
		},
		{
			name: "with body",
			err:  &StatusError{URL: "https://example.test/x", StatusCode: 404, Body: "gone"},
			want: "update: GET https://example.test/x: unexpected status 404: gone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTimeoutError(t *testing.T) {
	t.Parallel()

	err := &TimeoutError{URL: "https://example.test/x", Err: context.DeadlineExceeded}

	if want := "update: GET https://example.test/x: timed out: context deadline exceeded"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	if !err.Timeout() {
		t.Error("Timeout() = false")
	}

	if !err.Temporary() {
		t.Error("Temporary() = false")
	}
}

func TestTagError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *TagError
		want string
	}{
		{name: "without a cause", err: &TagError{Tag: "x"}, want: `update: invalid release tag "x"`},
		{
			name: "with a cause",
			err:  &TagError{Tag: "x", Err: errors.New("bad")},
			want: `update: invalid release tag "x": bad`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
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

func TestAssetNotFoundError(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("select: %w", &AssetNotFoundError{Tag: "v1.2.3", Name: "atk_linux_amd64.tar.gz"})
	var nf *AssetNotFoundError
	if !errors.As(err, &nf) {
		t.Fatal("errors.As did not find *AssetNotFoundError")
	}
	if nf.Tag != "v1.2.3" || nf.Name != "atk_linux_amd64.tar.gz" {
		t.Errorf("got Tag %q Name %q", nf.Tag, nf.Name)
	}
	if want := `update: release v1.2.3 has no asset "atk_linux_amd64.tar.gz"`; nf.Error() != want {
		t.Errorf("Error() = %q, want %q", nf.Error(), want)
	}
}

func TestChecksumAndSizeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		as     func(error) bool
		wantIn []string
	}{
		{
			name: "mismatch",
			err:  &ChecksumMismatchError{Name: "atk.tar.gz", Want: "aa11", Got: "bb22"},
			as:   func(err error) bool { var e *ChecksumMismatchError; return errors.As(err, &e) },
			wantIn: []string{
				"update: ", "atk.tar.gz", "aa11", "bb22",
			},
		},
		{
			name:   "not found",
			err:    &ChecksumNotFoundError{Name: "atk.tar.gz"},
			as:     func(err error) bool { var e *ChecksumNotFoundError; return errors.As(err, &e) },
			wantIn: []string{"update: ", "atk.tar.gz"},
		},
		{
			name:   "conflict",
			err:    &ChecksumConflictError{Name: "atk.tar.gz"},
			as:     func(err error) bool { var e *ChecksumConflictError; return errors.As(err, &e) },
			wantIn: []string{"update: ", "atk.tar.gz"},
		},
		{
			name:   "size limit",
			err:    &SizeLimitError{URL: "https://example.test/x", Limit: 4096},
			as:     func(err error) bool { var e *SizeLimitError; return errors.As(err, &e) },
			wantIn: []string{"update: ", "https://example.test/x", "4096"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("download: %w", tc.err)
			if !tc.as(wrapped) {
				t.Fatalf("errors.As did not find the error in %v", wrapped)
			}

			for _, want := range tc.wantIn {
				if !strings.Contains(tc.err.Error(), want) {
					t.Errorf("Error() = %q, missing %q", tc.err.Error(), want)
				}
			}
		})
	}
}

func TestErrAssetsNotListed_Is(t *testing.T) {
	t.Parallel()

	if !errors.Is(fmt.Errorf("select: %w", ErrAssetsNotListed), ErrAssetsNotListed) {
		t.Error("errors.Is does not match ErrAssetsNotListed through a wrap")
	}
}

func TestManagedInstallError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *ManagedInstallError
		want string
	}{
		{
			name: "path resolves to itself",
			err:  &ManagedInstallError{Path: "/opt/homebrew/bin/atk", Resolved: "/opt/homebrew/bin/atk", Manager: "Homebrew", Instead: "run `brew upgrade atk`"},
			want: "update: /opt/homebrew/bin/atk is managed by Homebrew; run `brew upgrade atk`",
		},
		{
			name: "path resolves elsewhere",
			err:  &ManagedInstallError{Path: "/usr/local/bin/atk", Resolved: "/usr/local/Cellar/atk/1/bin/atk", Manager: "Homebrew", Instead: "run `brew upgrade atk`"},
			want: "update: /usr/local/bin/atk (resolves to /usr/local/Cellar/atk/1/bin/atk) is managed by Homebrew; run `brew upgrade atk`",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManagedInstallError_As(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("check: %w", &ManagedInstallError{Path: "/p", Resolved: "/p", Manager: "Nix", Instead: "x"})

	var managed *ManagedInstallError
	if !errors.As(err, &managed) {
		t.Fatal("errors.As did not find *ManagedInstallError")
	}

	if managed.Manager != "Nix" {
		t.Errorf("Manager = %q, want Nix", managed.Manager)
	}
}
