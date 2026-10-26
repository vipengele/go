package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

// tarEntry is one entry of a tar archive a test builds: hdr is written as given, with Size set to
// the length of body for a regular file.
type tarEntry struct {
	hdr  tar.Header
	body []byte
}

func tarFile(name, body string) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755}, body: []byte(body)}
}

func tarBytes(name string, body []byte) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, body: body}
}

func tarDir(name string) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}}
}

func tarSpecial(name string, typeflag byte, linkname string) tarEntry {
	return tarEntry{hdr: tar.Header{Name: name, Typeflag: typeflag, Linkname: linkname, Mode: 0o644}}
}

// tarOf returns the uncompressed tar holding entries, in order.
func tarOf(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}

		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", hdr.Name, err)
		}

		if _, err := tw.Write(e.body); err != nil {
			t.Fatalf("Write(%q): %v", hdr.Name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}

	return buf.Bytes()
}

// gzipOf returns data gzip-compressed.
func gzipOf(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("gzip Write: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	return buf.Bytes()
}

// tarGzOf returns the tar.gz archive holding entries, in order.
func tarGzOf(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	return gzipOf(t, tarOf(t, entries...))
}

// noise returns n bytes that deflate barely compresses, the same for every call.
func noise(n int) []byte {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test data, not a secret

	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}

	return b
}

// rawTarHeader returns a ustar header block for name, of typeflag and body size, with a valid
// checksum, for headers [tar.Writer] refuses to write.
func rawTarHeader(name string, typeflag byte, size int) []byte {
	blk := make([]byte, 512)
	copy(blk[0:100], name)
	copy(blk[100:108], "0000644\x00")
	copy(blk[108:116], "0000000\x00")
	copy(blk[116:124], "0000000\x00")
	copy(blk[124:136], fmt.Sprintf("%011o\x00", size))
	copy(blk[136:148], "00000000000\x00")
	blk[156] = typeflag
	copy(blk[257:263], "ustar\x00")
	copy(blk[263:265], "00")

	copy(blk[148:156], "        ")

	sum := 0
	for _, b := range blk {
		sum += int(b)
	}

	copy(blk[148:156], fmt.Sprintf("%06o\x00 ", sum))

	return blk
}

// padBlock returns b padded with zeros to a multiple of the 512-byte tar block.
func padBlock(b []byte) []byte {
	if rem := len(b) % 512; rem != 0 {
		b = append(b, make([]byte, 512-rem)...)
	}

	return b
}

// paxRecord returns the PAX record "<length> key=value\n", its length counting itself.
func paxRecord(key, value string) string {
	rest := " " + key + "=" + value + "\n"
	for n := len(rest) + 1; ; n++ {
		if s := fmt.Sprintf("%d%s", n, rest); len(s) == n {
			return s
		}
	}
}

// tarGzConfig is a valid Config that names a tar.gz archive and the binary "atk".
func tarGzConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.GOOS = "linux"

	return c
}

// extractTarGzArchive runs Config.Extract on a fresh Archive holding data, with the Config config
// returns from tarGzConfig, and returns the staged binary's content. On an error it checks that
// the archive is removed and nothing is left staged.
func extractTarGzArchive(t *testing.T, ctx context.Context, data []byte, config func(Config) Config) ([]byte, error) {
	t.Helper()

	a, archiveDir := newExtractArchive(t, data)
	stageDir := t.TempDir()

	c := tarGzConfig(t)
	if config != nil {
		c = config(c)
	}

	s, err := c.Extract(ctx, a, stageDir)
	assertArchiveClosed(t, a, archiveDir)

	if err != nil {
		if s != nil {
			t.Errorf("Staged = %v, want nil", s)
		}

		assertDirEmpty(t, stageDir)

		return nil, err
	}

	keepAtCleanup(t, s)

	got, readErr := os.ReadFile(s.Name())
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}

	return got, nil
}

// withLimit returns a Config option setting MaxExtractBytes to limit.
func withLimit(limit int64) func(Config) Config {
	return func(c Config) Config { c.MaxExtractBytes = limit; return c }
}

func TestExtractTarGzFindsTheBinary(t *testing.T) {
	t.Parallel()

	binary := noise(100_000)
	longDir := strings.Repeat("d", 200)

	tests := []struct {
		name    string
		entries []tarEntry
		config  func(Config) Config
	}{
		{name: "at the root", entries: []tarEntry{tarFile("README.md", "docs"), tarBytes("atk", binary)}},
		{
			name:    "nested",
			entries: []tarEntry{tarDir("atk_linux_amd64/"), tarDir("atk_linux_amd64/bin/"), tarBytes("atk_linux_amd64/bin/atk", binary)},
		},
		{name: "dot prefix", entries: []tarEntry{tarDir("./"), tarDir("./bin/"), tarBytes("./bin/atk", binary)}},
		{name: "beside a directory named like it", entries: []tarEntry{tarDir("atk/"), tarBytes("bin/atk", binary)}},
		{name: "after a global PAX header", entries: []tarEntry{globalPAXHeader(""), tarBytes("atk", binary)}},
		{
			name:    "after a global PAX header with an absolute name",
			entries: []tarEntry{globalPAXHeader("/tmp/GlobalHead.1.1"), tarBytes("atk", binary)},
		},
		{
			name:    "on windows",
			entries: []tarEntry{tarFile("atk", "not for windows"), tarBytes("atk.exe", binary)},
			config:  func(c Config) Config { c.GOOS = "windows"; return c },
		},
		{
			name: "under a PAX long name",
			entries: []tarEntry{{
				hdr:  tar.Header{Name: longDir + "/atk", Typeflag: tar.TypeReg, Mode: 0o755, Format: tar.FormatPAX},
				body: binary,
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractTarGzArchive(t, context.Background(), tarGzOf(t, tc.entries...), tc.config)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if !bytes.Equal(got, binary) {
				t.Errorf("staged binary holds %d bytes, want the %d of the entry unchanged", len(got), len(binary))
			}
		})
	}
}

func globalPAXHeader(name string) tarEntry {
	return tarEntry{hdr: tar.Header{
		Name:       name,
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "release"},
	}}
}

func TestExtractTarGzLongNameIsPAX(t *testing.T) {
	t.Parallel()

	tarData := tarOf(t, tarEntry{
		hdr:  tar.Header{Name: strings.Repeat("d", 200) + "/atk", Typeflag: tar.TypeReg, Format: tar.FormatPAX},
		body: []byte("bin"),
	})

	if !bytes.Contains(tarData, []byte(" path=")) {
		t.Fatal("the long name is not written as a PAX path record")
	}
}

// wantUnsafeEntry checks err is an *UnsafeEntryError for entry, with a reason containing reason.
func wantUnsafeEntry(entry, reason string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		var unsafeErr *UnsafeEntryError
		if !errors.As(err, &unsafeErr) {
			t.Fatalf("err = %v, want *UnsafeEntryError", err)
		}

		if unsafeErr.Name != entry || !strings.Contains(unsafeErr.Reason, reason) {
			t.Errorf("UnsafeEntryError = {%q, %q}, want {%q, containing %q}", unsafeErr.Name, unsafeErr.Reason, entry, reason)
		}
	}
}

func wantErrorIs(target error) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		if !errors.Is(err, target) {
			t.Errorf("err = %v, want %v", err, target)
		}
	}
}

func wantBinaryNotFound(name string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		var notFound *BinaryNotFoundError
		if !errors.As(err, &notFound) || notFound.Name != name {
			t.Errorf("err = %v, want *BinaryNotFoundError for %q", err, name)
		}
	}
}

func TestExtractTarGzRejectsTheArchive(t *testing.T) {
	t.Parallel()

	bin := tarFile("atk", "binary")

	tests := []struct {
		name    string
		entries []tarEntry
		config  func(Config) Config
		check   func(*testing.T, error)
	}{
		{name: "parent component", entries: []tarEntry{tarFile("../atk", "x")}, check: wantUnsafeEntry("../atk", "..")},
		{
			name:    "parent component inside",
			entries: []tarEntry{tarFile("a/../../atk", "x")},
			check:   wantUnsafeEntry("a/../../atk", ".."),
		},
		{name: "absolute path", entries: []tarEntry{tarFile("/abs/atk", "x")}, check: wantUnsafeEntry("/abs/atk", "absolute")},
		{name: "backslash", entries: []tarEntry{tarFile(`a\atk`, "x")}, check: wantUnsafeEntry(`a\atk`, "backslash")},
		{name: "drive letter and backslash", entries: []tarEntry{tarFile(`C:\atk`, "x")}, check: wantUnsafeEntry(`C:\atk`, "backslash")},
		{name: "drive-relative path", entries: []tarEntry{tarFile("C:atk", "x")}, check: wantUnsafeEntry("C:atk", "drive")},
		{name: "unsafe directory", entries: []tarEntry{tarDir("../bin/"), bin}, check: wantUnsafeEntry("../bin/", "..")},
		{
			name:    "symbolic link",
			entries: []tarEntry{tarSpecial("atk", tar.TypeSymlink, "/usr/bin/atk")},
			check:   wantUnsafeEntry("atk", "symbolic link"),
		},
		{
			name:    "hard link",
			entries: []tarEntry{tarFile("x", "binary"), tarSpecial("atk", tar.TypeLink, "x")},
			check:   wantUnsafeEntry("atk", "hard link"),
		},
		{
			name:    "hard link to the binary",
			entries: []tarEntry{bin, tarSpecial("bin/other", tar.TypeLink, "atk")},
			check:   wantUnsafeEntry("bin/other", "hard link"),
		},
		{name: "character device", entries: []tarEntry{tarSpecial("dev/tty", tar.TypeChar, ""), bin}, check: wantUnsafeEntry("dev/tty", "not a regular file")},
		{name: "block device", entries: []tarEntry{tarSpecial("dev/sda", tar.TypeBlock, ""), bin}, check: wantUnsafeEntry("dev/sda", "not a regular file")},
		{name: "FIFO", entries: []tarEntry{tarSpecial("pipe", tar.TypeFifo, ""), bin}, check: wantUnsafeEntry("pipe", "not a regular file")},
		{
			name:    "unsafe entry after the binary",
			entries: []tarEntry{bin, tarFile("../escape", "x")},
			check:   wantUnsafeEntry("../escape", ".."),
		},
		{
			name:    "duplicate binary",
			entries: []tarEntry{tarFile("atk", "one"), tarDir("bin/"), tarFile("bin/atk", "two")},
			check: func(t *testing.T, err error) {
				t.Helper()

				var multiple *MultipleBinariesError
				if !errors.As(err, &multiple) {
					t.Fatalf("err = %v, want *MultipleBinariesError", err)
				}

				if want := []string{"atk", "bin/atk"}; multiple.Name != "atk" || !slicesEqual(multiple.Entries, want) {
					t.Errorf("MultipleBinariesError = {%q, %q}, want {%q, %q}", multiple.Name, multiple.Entries, "atk", want)
				}
			},
		},
		{name: "missing binary", entries: []tarEntry{tarFile("README.md", "docs")}, check: wantBinaryNotFound("atk")},
		{name: "empty archive", entries: nil, check: wantBinaryNotFound("atk")},
		{name: "directory named like the binary", entries: []tarEntry{tarDir("atk/")}, check: wantBinaryNotFound("atk")},
		{name: "name with a suffix", entries: []tarEntry{tarFile("atk.sh", "x"), tarFile("bin/xatk", "x")}, check: wantBinaryNotFound("atk")},
		{
			name:    "windows binary without .exe",
			entries: []tarEntry{bin},
			config:  func(c Config) Config { c.GOOS = "windows"; return c },
			check:   wantBinaryNotFound("atk.exe"),
		},
		{name: "empty binary", entries: []tarEntry{tarFile("bin/atk", "")}, check: wantErrorIs(ErrEmptyAsset)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := tarGzOf(t, tc.entries...)

			_, err := extractTarGzArchive(t, context.Background(), data, tc.config)
			if err == nil {
				t.Fatal("Extract succeeded, want an error")
			}

			tc.check(t, err)

			var dst bytes.Buffer

			c := tarGzConfig(t)
			if tc.config != nil {
				c = tc.config(c)
			}

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			if err := extractTarGz(context.Background(), bytes.NewReader(data), int64(len(data)), binaryFileName(r), math.MaxInt64, &dst); err == nil {
				t.Fatal("extractTarGz succeeded, want an error")
			}

			if dst.Len() != 0 {
				t.Errorf("extractTarGz wrote %d bytes to dst, want none", dst.Len())
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestExtractTarGzRejectsANULInAPAXPath(t *testing.T) {
	t.Parallel()

	// A ustar name field ends at its first NUL, so a NUL reaches a name only through a PAX path
	// record, which tar.Writer refuses to write.
	record := paxRecord("path", "bin\x00/atk")

	var raw []byte
	raw = append(raw, rawTarHeader("PaxHeaders/atk", tar.TypeXHeader, len(record))...)
	raw = append(raw, padBlock([]byte(record))...)
	raw = append(raw, rawTarHeader("atk", tar.TypeReg, 3)...)
	raw = append(raw, padBlock([]byte("bin"))...)
	raw = append(raw, make([]byte, 1024)...)

	data := gzipOf(t, raw)

	_, err := extractTarGzArchive(t, context.Background(), data, nil)
	if !errors.Is(err, tar.ErrHeader) {
		t.Errorf("err = %v, want tar.ErrHeader", err)
	}

	// The same archive with a NUL-free path extracts, so the NUL alone is what is refused.
	record = paxRecord("path", "bin/atk")
	ok := append(rawTarHeader("PaxHeaders/atk", tar.TypeXHeader, len(record)), padBlock([]byte(record))...)
	ok = append(ok, raw[len(ok):]...)

	got, err := extractTarGzArchive(t, context.Background(), gzipOf(t, ok), nil)
	if err != nil || string(got) != "bin" {
		t.Errorf("Extract = %q, %v, want %q, nil", got, err, "bin")
	}
}

func TestExtractTarGzBudget(t *testing.T) {
	t.Parallel()

	bomb := make([]byte, 1<<20)
	small := make([]byte, 10<<10)
	bin := tarFile("atk", "binary")

	exact := tarOf(t, tarFile("README.md", "docs"), bin)

	tests := []struct {
		name    string
		entries []tarEntry
		data    []byte
		limit   int64
		entry   string
	}{
		{name: "bomb before the binary", entries: []tarEntry{tarBytes("docs/big", bomb), bin}, limit: 64 << 10, entry: "docs/big"},
		{name: "bomb after the binary", entries: []tarEntry{bin, tarBytes("docs/big", bomb)}, limit: 64 << 10, entry: "docs/big"},
		{name: "bomb in the binary", entries: []tarEntry{tarFile("README.md", "docs"), tarBytes("atk", bomb)}, limit: 64 << 10, entry: "atk"},
		{
			name:    "across several entries",
			entries: []tarEntry{tarBytes("a", small), tarBytes("b", small), bin},
			limit:   20_000,
			entry:   "b",
		},
		{name: "in the first header", entries: []tarEntry{bin}, limit: 100, entry: ""},
		{name: "one byte under the tar", data: gzipOf(t, exact), limit: int64(len(exact)) - 1, entry: "atk"},
		{
			name:  "trailing data after the tar",
			data:  gzipOf(t, append(append([]byte{}, exact...), make([]byte, 4096)...)),
			limit: int64(len(exact)),
			entry: "atk",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := tc.data
			if data == nil {
				data = tarGzOf(t, tc.entries...)
			}

			_, err := extractTarGzArchive(t, context.Background(), data, withLimit(tc.limit))
			wantExtractLimit(tc.entry, tc.limit)(t, err)
		})
	}
}

func TestExtractTarGzAtTheLimit(t *testing.T) {
	t.Parallel()

	tarData := tarOf(t, tarFile("README.md", "docs"), tarBytes("atk", noise(4096)))

	got, err := extractTarGzArchive(t, context.Background(), gzipOf(t, tarData), withLimit(int64(len(tarData))))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(got) != 4096 {
		t.Errorf("staged binary holds %d bytes, want 4096", len(got))
	}
}

func TestExtractTarGzRejectsACorruptStream(t *testing.T) {
	t.Parallel()

	data := tarGzOf(t, tarFile("README.md", "docs"), tarBytes("atk", noise(64<<10)))

	var zipped bytes.Buffer

	zw := zip.NewWriter(&zipped)
	if _, err := zw.Create("atk"); err != nil {
		t.Fatalf("zip Create: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}

	badCRC := append([]byte{}, data...)
	badCRC[len(badCRC)-8] ^= 0xff

	tests := []struct {
		name  string
		data  []byte
		check func(*testing.T, error)
	}{
		{name: "empty", data: nil, check: wantErrorIs(ErrNotGzip)},
		{name: "one byte of magic", data: data[:1], check: wantErrorIs(ErrNotGzip)},
		{name: "plain bytes", data: []byte("#!/bin/sh\necho atk\n"), check: wantErrorIs(ErrNotGzip)},
		{name: "uncompressed tar", data: tarOf(t, tarFile("atk", "binary")), check: wantErrorIs(ErrNotGzip)},
		{name: "zip", data: zipped.Bytes(), check: wantErrorIs(ErrNotGzip)},
		{name: "cut after the magic", data: data[:2], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "cut in the gzip header", data: data[:5], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "cut early in the deflate stream", data: data[:20], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "cut in the binary", data: data[:len(data)/2], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "cut in the trailer", data: data[:len(data)-4], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "no trailer", data: data[:len(data)-8], check: wantErrorIs(ErrTruncatedGzip)},
		{name: "wrong checksum in the trailer", data: badCRC, check: wantErrorIs(gzip.ErrChecksum)},
		{name: "garbage after the stream", data: append(append([]byte{}, data...), "not a second gzip member"...), check: wantErrorIs(gzip.ErrHeader)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractTarGzArchive(t, context.Background(), tc.data, nil)
			if err == nil {
				t.Fatal("Extract succeeded, want an error")
			}

			tc.check(t, err)

			if errors.Is(err, ErrNotGzip) && errors.Is(err, ErrTruncatedGzip) {
				t.Errorf("err = %v, matches both ErrNotGzip and ErrTruncatedGzip", err)
			}
		})
	}
}

func TestExtractTarGzCancellation(t *testing.T) {
	t.Parallel()

	data := tarGzOf(t, tarFile("README.md", "docs"), tarBytes("atk", noise(1<<20)))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  func() context.Context
	}{
		{name: "before extracting", ctx: func() context.Context { return cancelled }},
		{name: "before the first entry", ctx: func() context.Context { return newCountdownContext(1) }},
		{name: "while scanning", ctx: func() context.Context { return newCountdownContext(8) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := extractTarGzArchive(t, tc.ctx(), data, nil); !errors.Is(err, context.Canceled) {
				t.Errorf("Extract = %v, want context.Canceled", err)
			}
		})
	}
}

func TestExtractTarGzCancellationAtEveryRead(t *testing.T) {
	t.Parallel()

	data := tarGzOf(t, tarFile("README.md", "docs"), tarBytes("atk", noise(256<<10)))

	// Every count of live checks either finishes the extraction or stops it with ctx's error;
	// the counts run past the last check, so the final one succeeds.
	var succeeded bool

	for live := int64(0); live < 1000 && !succeeded; live++ {
		var dst bytes.Buffer

		err := extractTarGz(newCountdownContext(live), bytes.NewReader(data), int64(len(data)), "atk", math.MaxInt64, &dst)
		switch {
		case err == nil:
			succeeded = true
		case !errors.Is(err, context.Canceled):
			t.Fatalf("extractTarGz with %d live checks = %v, want nil or context.Canceled", live, err)
		}
	}

	if !succeeded {
		t.Error("extractTarGz never succeeded")
	}
}

func TestBudgetReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		limit   int64
		wantErr bool
	}{
		{name: "empty", src: "", limit: 0},
		{name: "under", src: "abc", limit: 4},
		{name: "exactly", src: "abcd", limit: 4},
		{name: "over", src: "abcde", limit: 4, wantErr: true},
		{name: "zero limit", src: "a", limit: 0, wantErr: true},
		{name: "unbounded", src: "abcde", limit: math.MaxInt64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := &budgetReader{r: strings.NewReader(tc.src), limit: tc.limit, name: "entry"}

			got, err := io.ReadAll(b)
			if !tc.wantErr {
				if err != nil || string(got) != tc.src {
					t.Errorf("ReadAll = %q, %v, want %q, nil", got, err, tc.src)
				}

				return
			}

			wantExtractLimit("entry", tc.limit)(t, err)

			if int64(len(got)) > tc.limit {
				t.Errorf("read %d bytes, want at most the limit %d", len(got), tc.limit)
			}

			if _, err := b.Read(make([]byte, 1)); err == nil {
				t.Error("Read after the limit = nil, want the limit error again")
			}
		})
	}
}
