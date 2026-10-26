package update

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"os"
	"testing"
)

// zipEntry is one entry of a zip archive a test builds. hdr is written as given; a raw entry is
// written with [zip.Writer.CreateRaw], its body already compressed and its sizes and checksum
// taken from hdr unchanged, so they can disagree with the body.
type zipEntry struct {
	hdr  zip.FileHeader
	body []byte
	raw  bool
}

// zipFile returns a deflated regular file of mode 0755.
func zipFile(name string, body []byte) zipEntry {
	e := zipEntry{hdr: zip.FileHeader{Name: name, Method: zip.Deflate}, body: body}
	e.hdr.SetMode(0o755)

	return e
}

// zipStored returns a regular file stored without compression.
func zipStored(name string, body []byte) zipEntry {
	e := zipFile(name, body)
	e.hdr.Method = zip.Store

	return e
}

func zipDir(name string) zipEntry {
	e := zipEntry{hdr: zip.FileHeader{Name: name}}
	e.hdr.SetMode(fs.ModeDir | 0o755)

	return e
}

// zipSpecial returns an entry whose mode is mode, with body as its content.
func zipSpecial(name string, mode fs.FileMode, body string) zipEntry {
	e := zipEntry{hdr: zip.FileHeader{Name: name}, body: []byte(body)}
	e.hdr.SetMode(mode)

	return e
}

// zipRaw returns a raw entry: body is written as the entry's compressed data under method, with the
// CRC-32 of plain, and declared as uncompressing to declared bytes.
func zipRaw(name string, method uint16, body, plain []byte, declared uint64) zipEntry {
	e := zipEntry{
		hdr: zip.FileHeader{
			Name:               name,
			Method:             method,
			CRC32:              crc32.ChecksumIEEE(plain),
			CompressedSize64:   uint64(len(body)),
			UncompressedSize64: declared,
		},
		body: body,
		raw:  true,
	}
	e.hdr.SetMode(0o755)

	return e
}

// deflateOf returns data compressed with raw deflate, as a zip entry holds it.
func deflateOf(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatalf("flate NewWriter: %v", err)
	}

	if _, err := fw.Write(data); err != nil {
		t.Fatalf("flate Write: %v", err)
	}

	if err := fw.Close(); err != nil {
		t.Fatalf("flate Close: %v", err)
	}

	return buf.Bytes()
}

// zipOf returns the zip archive holding entries, in order.
func zipOf(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		hdr := e.hdr

		var (
			w   io.Writer
			err error
		)

		if e.raw {
			w, err = zw.CreateRaw(&hdr)
		} else {
			w, err = zw.CreateHeader(&hdr)
		}

		if err != nil {
			t.Fatalf("Create(%q): %v", hdr.Name, err)
		}

		if _, err := w.Write(e.body); err != nil {
			t.Fatalf("Write(%q): %v", hdr.Name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}

	return buf.Bytes()
}

// zipConfig is a valid Config that names a zip archive and the binary "atk".
func zipConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.GOOS = "linux"
	c.AssetExt = "zip"

	return c
}

// extractZipArchive runs Config.Extract on a fresh Archive holding data, with the Config config
// returns from zipConfig, and returns the staged binary's content. On an error it checks that the
// archive is removed and nothing is left staged.
func extractZipArchive(t *testing.T, ctx context.Context, data []byte, config func(Config) Config) ([]byte, error) {
	t.Helper()

	a, archiveDir := newExtractArchive(t, data)
	stageDir := t.TempDir()

	c := zipConfig(t)
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

// extractZipDirect runs extractZip on data for the binary name under limit, returning what it
// wrote to dst and its error.
func extractZipDirect(ctx context.Context, data []byte, name string, limit int64) ([]byte, error) {
	var dst bytes.Buffer

	err := extractZip(ctx, bytes.NewReader(data), int64(len(data)), name, limit, &dst)

	return dst.Bytes(), err
}

func TestExtractZipFindsTheBinary(t *testing.T) {
	t.Parallel()

	binary := noise(100_000)

	tests := []struct {
		name    string
		entries []zipEntry
		config  func(Config) Config
	}{
		{name: "at the root", entries: []zipEntry{zipFile("README.md", []byte("docs")), zipFile("atk", binary)}},
		{name: "stored", entries: []zipEntry{zipStored("atk", binary)}},
		{
			name:    "nested",
			entries: []zipEntry{zipDir("atk_linux_amd64/"), zipDir("atk_linux_amd64/bin/"), zipFile("atk_linux_amd64/bin/atk", binary)},
		},
		{name: "dot prefix", entries: []zipEntry{zipDir("./"), zipDir("./bin/"), zipFile("./bin/atk", binary)}},
		{name: "beside a directory named like it", entries: []zipEntry{zipDir("atk/"), zipDir("dir/atk/"), zipFile("bin/atk", binary)}},
		{
			name:    "beside an encrypted entry that is not the binary",
			entries: []zipEntry{encrypted(zipRaw("secret", zip.Store, []byte("xx"), []byte("xx"), 2)), zipFile("atk", binary)},
		},
		{
			name:    "on windows",
			entries: []zipEntry{zipFile("atk", []byte("not for windows")), zipFile("atk.exe", binary)},
			config:  func(c Config) Config { c.GOOS = "windows"; return c },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractZipArchive(t, context.Background(), zipOf(t, tc.entries...), tc.config)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if !bytes.Equal(got, binary) {
				t.Errorf("staged binary holds %d bytes, want the %d of the entry unchanged", len(got), len(binary))
			}
		})
	}
}

// encrypted returns e with the general-purpose flag that marks its body encrypted.
func encrypted(e zipEntry) zipEntry {
	e.hdr.Flags |= zipFlagEncrypted

	return e
}

func TestExtractZipRejectsTheArchive(t *testing.T) {
	t.Parallel()

	bin := zipFile("atk", []byte("binary"))

	tests := []struct {
		name    string
		entries []zipEntry
		config  func(Config) Config
		check   func(*testing.T, error)
	}{
		{name: "parent component", entries: []zipEntry{zipFile("../atk", []byte("x"))}, check: wantUnsafeEntry("../atk", "..")},
		{
			name:    "parent component inside",
			entries: []zipEntry{zipFile("a/../../atk", []byte("x"))},
			check:   wantUnsafeEntry("a/../../atk", ".."),
		},
		{
			name:    "parent component that stays inside",
			entries: []zipEntry{zipFile("a/../atk", []byte("x"))},
			check:   wantUnsafeEntry("a/../atk", ".."),
		},
		{name: "absolute path", entries: []zipEntry{zipFile("/abs/atk", []byte("x"))}, check: wantUnsafeEntry("/abs/atk", "absolute")},
		{name: "backslash", entries: []zipEntry{zipFile(`a\atk`, []byte("x"))}, check: wantUnsafeEntry(`a\atk`, "backslash")},
		{name: "drive letter and backslash", entries: []zipEntry{zipFile(`C:\atk`, []byte("x"))}, check: wantUnsafeEntry(`C:\atk`, "backslash")},
		{name: "drive-relative path", entries: []zipEntry{zipFile("C:atk", []byte("x"))}, check: wantUnsafeEntry("C:atk", "drive")},
		{name: "NUL byte", entries: []zipEntry{zipFile("bin\x00/atk", []byte("x")), bin}, check: wantUnsafeEntry("bin\x00/atk", "NUL")},
		{name: "empty name", entries: []zipEntry{zipFile("", []byte("x")), bin}, check: wantUnsafeEntry("", "empty")},
		{name: "unsafe directory", entries: []zipEntry{zipDir("../bin/"), bin}, check: wantUnsafeEntry("../bin/", "..")},
		{
			name:    "symbolic link",
			entries: []zipEntry{zipSpecial("atk", fs.ModeSymlink|0o777, "/usr/bin/atk")},
			check:   wantUnsafeEntry("atk", "symbolic link"),
		},
		{
			name:    "symbolic link beside the binary",
			entries: []zipEntry{bin, zipSpecial("bin/other", fs.ModeSymlink|0o777, "../atk")},
			check:   wantUnsafeEntry("bin/other", "symbolic link"),
		},
		{
			name:    "character device",
			entries: []zipEntry{zipSpecial("dev/tty", fs.ModeDevice|fs.ModeCharDevice|0o644, ""), bin},
			check:   wantUnsafeEntry("dev/tty", "not a regular file"),
		},
		{
			name:    "block device",
			entries: []zipEntry{zipSpecial("dev/sda", fs.ModeDevice|0o644, ""), bin},
			check:   wantUnsafeEntry("dev/sda", "not a regular file"),
		},
		{name: "named pipe", entries: []zipEntry{zipSpecial("pipe", fs.ModeNamedPipe|0o644, ""), bin}, check: wantUnsafeEntry("pipe", "not a regular file")},
		{name: "socket", entries: []zipEntry{zipSpecial("sock", fs.ModeSocket|0o644, ""), bin}, check: wantUnsafeEntry("sock", "not a regular file")},
		{
			name:    "unsafe entry after the binary",
			entries: []zipEntry{bin, zipFile("../escape", []byte("x"))},
			check:   wantUnsafeEntry("../escape", ".."),
		},
		{
			name:    "duplicate binary at two depths",
			entries: []zipEntry{zipFile("atk", []byte("one")), zipDir("bin/"), zipFile("bin/atk", []byte("two"))},
			check:   wantMultipleBinaries("atk", "atk", "bin/atk"),
		},
		{
			name:    "duplicate binary under one name",
			entries: []zipEntry{zipFile("bin/atk", []byte("one")), zipFile("bin/atk", []byte("two"))},
			check:   wantMultipleBinaries("atk", "bin/atk", "bin/atk"),
		},
		{name: "missing binary", entries: []zipEntry{zipFile("README.md", []byte("docs"))}, check: wantBinaryNotFound("atk")},
		{name: "empty archive", entries: nil, check: wantBinaryNotFound("atk")},
		{name: "only a directory", entries: []zipEntry{zipDir("dir/")}, check: wantBinaryNotFound("atk")},
		{name: "directory named like the binary", entries: []zipEntry{zipDir("atk/")}, check: wantBinaryNotFound("atk")},
		{
			name:    "name with a suffix",
			entries: []zipEntry{zipFile("atk.sh", []byte("x")), zipFile("bin/xatk", []byte("x"))},
			check:   wantBinaryNotFound("atk"),
		},
		{
			name:    "windows binary without .exe",
			entries: []zipEntry{bin},
			config:  func(c Config) Config { c.GOOS = "windows"; return c },
			check:   wantBinaryNotFound("atk.exe"),
		},
		{name: "empty binary", entries: []zipEntry{zipFile("bin/atk", nil)}, check: wantErrorIs(ErrEmptyAsset)},
		{
			name:    "encrypted binary",
			entries: []zipEntry{encrypted(zipRaw("bin/atk", zip.Store, []byte("binary"), []byte("binary"), 6))},
			check:   wantUnsafeEntry("bin/atk", "encrypted"),
		},
		{
			name:    "unsupported compression method",
			entries: []zipEntry{zipRaw("atk", 99, []byte("binary"), []byte("binary"), 6)},
			check:   wantErrorIs(zip.ErrAlgorithm),
		},
		{
			name:    "wrong checksum",
			entries: []zipEntry{zipRaw("atk", zip.Store, []byte("binary"), []byte("other!"), 6)},
			check:   wantErrorIs(zip.ErrChecksum),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := zipOf(t, tc.entries...)

			_, err := extractZipArchive(t, context.Background(), data, tc.config)
			if err == nil {
				t.Fatal("Extract succeeded, want an error")
			}

			tc.check(t, err)

			c := zipConfig(t)
			if tc.config != nil {
				c = tc.config(c)
			}

			r, err := c.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			written, err := extractZipDirect(context.Background(), data, binaryFileName(r), math.MaxInt64)
			if err == nil {
				t.Fatal("extractZip succeeded, want an error")
			}

			if errors.Is(err, zip.ErrChecksum) {
				// The checksum is known only once the whole body has been copied.
				return
			}

			if len(written) != 0 {
				t.Errorf("extractZip wrote %d bytes to dst, want none", len(written))
			}
		})
	}
}

func wantMultipleBinaries(name string, entries ...string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()

		var multiple *MultipleBinariesError
		if !errors.As(err, &multiple) {
			t.Fatalf("err = %v, want *MultipleBinariesError", err)
		}

		if multiple.Name != name || !slicesEqual(multiple.Entries, entries) {
			t.Errorf("MultipleBinariesError = {%q, %q}, want {%q, %q}", multiple.Name, multiple.Entries, name, entries)
		}
	}
}

// TestExtractZipMapsErrInsecurePath sets GODEBUG, so it cannot run in parallel.
func TestExtractZipMapsErrInsecurePath(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")

	tests := []struct {
		name     string
		entry    string
		reason   string
		insecure bool
	}{
		{name: "parent component", entry: "../atk", reason: "..", insecure: true},
		{name: "parent component inside", entry: "a/../../atk", reason: "..", insecure: true},
		{name: "absolute path", entry: "/abs/atk", reason: "absolute", insecure: true},
		{name: "backslash", entry: `a\atk`, reason: "backslash", insecure: true},
		// zip counts a ".." that stays inside the archive as local; the policy refuses it still.
		{name: "parent component that stays inside", entry: "a/../atk", reason: "..", insecure: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := zipOf(t, zipFile("bin/atk", []byte("binary")), zipFile(tc.entry, []byte("x")))

			if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); errors.Is(err, zip.ErrInsecurePath) != tc.insecure {
				t.Fatalf("zip.NewReader = %v, want ErrInsecurePath %v", err, tc.insecure)
			}

			_, err := extractZipArchive(t, context.Background(), data, nil)
			wantUnsafeEntry(tc.entry, tc.reason)(t, err)

			if errors.Is(err, zip.ErrInsecurePath) != tc.insecure {
				t.Errorf("err = %v, want errors.Is ErrInsecurePath %v", err, tc.insecure)
			}
		})
	}

	got, err := extractZipArchive(t, context.Background(), zipOf(t, zipDir("./bin/"), zipFile("./bin/atk", []byte("binary"))), nil)
	if err != nil || string(got) != "binary" {
		t.Errorf("Extract of a local archive = %q, %v, want %q, nil", got, err, "binary")
	}
}

func TestScanZipRejectsAnInsecureArchiveWithNoUnsafeEntry(t *testing.T) {
	t.Parallel()

	data := zipOf(t, zipFile("atk", []byte("binary")))

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	_, err = scanZip(context.Background(), zr.File, "atk", true)

	var unsafeErr *UnsafeEntryError
	if !errors.As(err, &unsafeErr) || !errors.Is(err, zip.ErrInsecurePath) {
		t.Errorf("scanZip = %v, want an *UnsafeEntryError matching zip.ErrInsecurePath", err)
	}
}

func TestZipLocalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		local bool
	}{
		{name: "", local: true},
		{name: "atk", local: true},
		{name: "./bin/atk", local: true},
		{name: "a/../atk", local: true},
		{name: "../atk"},
		{name: "/atk"},
		{name: `bin\atk`},
	}

	for _, tc := range tests {
		if got := zipLocalName(tc.name); got != tc.local {
			t.Errorf("zipLocalName(%q) = %v, want %v", tc.name, got, tc.local)
		}
	}
}

func TestExtractZipBudget(t *testing.T) {
	t.Parallel()

	bomb := make([]byte, 1<<20)
	deflatedBomb := deflateOf(t, bomb)
	binary := noise(4096)

	tests := []struct {
		name    string
		entries []zipEntry
		limit   int64
		entry   string
	}{
		{name: "bomb in the binary", entries: []zipEntry{zipFile("README.md", []byte("docs")), zipFile("bin/atk", bomb)}, limit: 64 << 10, entry: "bin/atk"},
		{name: "one byte over", entries: []zipEntry{zipFile("atk", binary)}, limit: int64(len(binary)) - 1, entry: "atk"},
		{
			name:    "bomb declared one byte over the limit",
			entries: []zipEntry{zipRaw("atk", zip.Deflate, deflatedBomb, bomb, 64<<10+1)},
			limit:   64 << 10,
			entry:   "atk",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractZipArchive(t, context.Background(), zipOf(t, tc.entries...), withLimit(tc.limit))
			wantExtractLimit(tc.entry, tc.limit)(t, err)
		})
	}
}

func TestExtractZipBudgetIsPerSelectedEntry(t *testing.T) {
	t.Parallel()

	binary := noise(4096)
	bomb := make([]byte, 1<<20)

	tests := []struct {
		name    string
		entries []zipEntry
	}{
		{name: "binary alone"},
		{name: "bomb before the binary", entries: []zipEntry{zipFile("docs/big", bomb)}},
		{name: "bomb declaring a small size", entries: []zipEntry{zipRaw("docs/big", zip.Deflate, deflateOf(t, bomb), bomb, 10)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := zipOf(t, append(tc.entries, zipFile("atk", binary))...)

			got, err := extractZipArchive(t, context.Background(), data, withLimit(int64(len(binary))))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			if !bytes.Equal(got, binary) {
				t.Errorf("staged binary holds %d bytes, want the %d of the entry unchanged", len(got), len(binary))
			}
		})
	}
}

func TestExtractZipDoesNotTrustTheDeclaredSize(t *testing.T) {
	t.Parallel()

	bomb := make([]byte, 1<<20)
	body := []byte("binary")

	tests := []struct {
		name  string
		entry zipEntry
		limit int64
		want  error
	}{
		// archive/zip refuses a body past its declared size before the limit is reached.
		{name: "deflated bomb declared small", entry: zipRaw("atk", zip.Deflate, deflateOf(t, bomb), bomb, 10), limit: 64 << 10, want: zip.ErrFormat},
		{name: "stored body declared small", entry: zipRaw("atk", zip.Store, body, body, 3), limit: 64 << 10, want: zip.ErrFormat},
		{name: "body declared empty", entry: zipRaw("atk", zip.Store, body, body, 0), limit: 64 << 10, want: zip.ErrFormat},
		// A declared size over the limit does not reject the entry; the body ending early does.
		{name: "small body declared over the limit", entry: zipRaw("atk", zip.Store, body, body, 1<<30), limit: 64 << 10, want: io.ErrUnexpectedEOF},
		{name: "empty body declared non-empty", entry: zipRaw("atk", zip.Store, nil, nil, 6), limit: 64 << 10, want: io.ErrUnexpectedEOF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractZipArchive(t, context.Background(), zipOf(t, tc.entry), withLimit(tc.limit))
			if !errors.Is(err, tc.want) {
				t.Errorf("Extract = %v, want %v", err, tc.want)
			}

			var limitErr *ExtractLimitError
			if errors.As(err, &limitErr) {
				t.Errorf("err = %v, want no *ExtractLimitError from a declared size", err)
			}
		})
	}
}

func TestExtractZipAtTheLimit(t *testing.T) {
	t.Parallel()

	binary := noise(4096)

	for _, e := range []zipEntry{zipFile("atk", binary), zipStored("atk", binary)} {
		got, err := extractZipArchive(t, context.Background(), zipOf(t, e), withLimit(int64(len(binary))))
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}

		if !bytes.Equal(got, binary) {
			t.Errorf("staged binary holds %d bytes, want the %d of the entry unchanged", len(got), len(binary))
		}
	}
}

func TestExtractZipRejectsAnInvalidArchive(t *testing.T) {
	t.Parallel()

	data := zipOf(t, zipFile("README.md", []byte("docs")), zipStored("atk", noise(64<<10)))

	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "plain bytes", data: []byte("#!/bin/sh\necho atk\n")},
		{name: "tar.gz", data: tarGzOf(t, tarFile("atk", "binary"))},
		{name: "no end of central directory", data: data[:len(data)-22]},
		{name: "cut in the binary", data: data[:len(data)/2]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractZipArchive(t, context.Background(), tc.data, nil)
			if !errors.Is(err, zip.ErrFormat) {
				t.Errorf("Extract = %v, want zip.ErrFormat", err)
			}
		})
	}
}

func TestExtractZipCancellation(t *testing.T) {
	t.Parallel()

	data := zipOf(t, zipFile("README.md", []byte("docs")), zipFile("atk", noise(1<<20)))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := extractZipArchive(t, cancelled, data, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Extract = %v, want context.Canceled", err)
	}

	written, err := extractZipDirect(newCountdownContext(0), data, "atk", math.MaxInt64)
	if !errors.Is(err, context.Canceled) || len(written) != 0 {
		t.Errorf("extractZip before the first entry = %v, %d bytes written, want context.Canceled and none", err, len(written))
	}
}

func TestExtractZipCancellationAtEveryRead(t *testing.T) {
	t.Parallel()

	data := zipOf(t, zipFile("README.md", []byte("docs")), zipFile("atk", noise(256<<10)))

	// Every count of live checks either finishes the extraction or stops it with ctx's error;
	// the counts run past the last check, so the final one succeeds.
	var succeeded bool

	for live := int64(0); live < 1000 && !succeeded; live++ {
		_, err := extractZipDirect(newCountdownContext(live), data, "atk", math.MaxInt64)
		switch {
		case err == nil:
			succeeded = true
		case !errors.Is(err, context.Canceled):
			t.Fatalf("extractZip with %d live checks = %v, want nil or context.Canceled", live, err)
		}
	}

	if !succeeded {
		t.Error("extractZip never succeeded")
	}
}
