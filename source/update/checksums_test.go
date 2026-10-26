package update

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseChecksum(t *testing.T) {
	t.Parallel()

	const (
		digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		digestC = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)

	tests := []struct {
		name  string
		input string
		asset string
		want  string
		err   any // a typed error pointer for errors.As, or nil
	}{
		{name: "plain", input: digestA + "  foo.tar.gz\n", asset: "foo.tar.gz", want: digestA},
		{name: "crlf", input: digestA + "  foo.tar.gz\r\n" + digestB + "  bar.tar.gz\r\n", asset: "foo.tar.gz", want: digestA},
		{name: "binary marker", input: digestA + " *foo.tar.gz\n", asset: "foo.tar.gz", want: digestA},
		{name: "dot slash", input: digestA + "  ./foo.tar.gz\n", asset: "foo.tar.gz", want: digestA},
		{name: "uppercase", input: strings.ToUpper(digestC) + "  foo.tar.gz\n", asset: "foo.tar.gz", want: digestC},
		{name: "spaces in name", input: digestA + "  my tool 1.0.tar.gz\n", asset: "my tool 1.0.tar.gz", want: digestA},
		{name: "no trailing newline", input: digestB + "  foo.tar.gz", asset: "foo.tar.gz", want: digestB},
		{name: "blank lines", input: "\n\n" + digestA + "  foo.tar.gz\n\n", asset: "foo.tar.gz", want: digestA},
		{
			name:  "duplicate same digest",
			input: digestA + "  foo.tar.gz\n" + strings.ToUpper(digestA) + " *./foo.tar.gz\n",
			asset: "foo.tar.gz",
			want:  digestA,
		},
		{
			name:  "duplicate different digest",
			input: digestA + "  foo.tar.gz\n" + digestB + "  foo.tar.gz\n",
			asset: "foo.tar.gz",
			err:   new(*ChecksumConflictError),
		},
		{
			name: "malformed lines skipped",
			input: "# comment\n" + "abc  foo.tar.gz\n" + digestB + "\n" + digestB + "  \n" +
				strings.Repeat("z", 64) + "  foo.tar.gz\n" + digestB + "foo.tar.gz\n" +
				digestA + "  foo.tar.gz\n",
			asset: "foo.tar.gz",
			want:  digestA,
		},
		{name: "missing entry", input: digestA + "  other.tar.gz\n", asset: "foo.tar.gz", err: new(*ChecksumNotFoundError)},
		{name: "empty input", input: "", asset: "foo.tar.gz", err: new(*ChecksumNotFoundError)},
		{
			name:  "name matched exactly",
			input: digestA + "  foo.tar.gz.sig\n" + digestB + "  xfoo.tar.gz\n",
			asset: "foo.tar.gz",
			err:   new(*ChecksumNotFoundError),
		},
		{
			name:  "name matched exactly beside a longer name",
			input: digestA + "  foo.tar.gz.sig\n" + digestB + "  foo.tar.gz\n",
			asset: "foo.tar.gz",
			want:  digestB,
		},
		{
			name:  "cap exceeded",
			input: digestA + "  foo.tar.gz\n" + strings.Repeat("x", maxChecksumsBytes),
			asset: "foo.tar.gz",
			err:   new(*SizeLimitError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseChecksum(strings.NewReader(tt.input), tt.asset)

			switch target := tt.err.(type) {
			case nil:
				if err != nil {
					t.Fatalf("parseChecksum() error = %v", err)
				}

				if hex.EncodeToString(got) != tt.want {
					t.Errorf("parseChecksum() = %x, want %s", got, tt.want)
				}
			case **ChecksumConflictError:
				if !errors.As(err, target) || (*target).Name != tt.asset {
					t.Errorf("parseChecksum() error = %v, want *ChecksumConflictError for %q", err, tt.asset)
				}
			case **ChecksumNotFoundError:
				if !errors.As(err, target) || (*target).Name != tt.asset {
					t.Errorf("parseChecksum() error = %v, want *ChecksumNotFoundError for %q", err, tt.asset)
				}
			case **SizeLimitError:
				if !errors.As(err, target) || (*target).Limit != maxChecksumsBytes || (*target).URL != "" {
					t.Errorf("parseChecksum() error = %v, want *SizeLimitError with the cap and no URL", err)
				}
			}
		})
	}
}

func TestParseChecksumAtCap(t *testing.T) {
	t.Parallel()

	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	line := digest + "  foo.tar.gz\n"
	input := line + strings.Repeat("\n", maxChecksumsBytes-len(line))

	got, err := parseChecksum(strings.NewReader(input), "foo.tar.gz")
	if err != nil {
		t.Fatalf("parseChecksum() error = %v", err)
	}

	if hex.EncodeToString(got) != digest {
		t.Errorf("parseChecksum() = %x, want %s", got, digest)
	}
}

func TestParseChecksumReadError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	_, err := parseChecksum(iotest.ErrReader(boom), "foo.tar.gz")
	if !errors.Is(err, boom) {
		t.Errorf("parseChecksum() error = %v, want %v", err, boom)
	}
}
