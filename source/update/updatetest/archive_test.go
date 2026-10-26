package updatetest_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

// entries is a set with an executable, a non-executable file in a subdirectory and an empty file.
func entries() []updatetest.Entry {
	return []updatetest.Entry{
		{Name: "tool", Mode: 0o755, Body: []byte("#!/bin/sh\necho v1.2.3\n")},
		{Name: "doc/README.md", Mode: 0o644, Body: []byte("# tool\n")},
		{Name: "empty", Mode: 0o600, Body: nil},
	}
}

// readTarGz returns every entry of a gzip-compressed tar archive, failing t on a non-regular one.
func readTarGz(t *testing.T, data []byte) []updatetest.Entry {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	tr := tar.NewReader(gz)
	var got []updatetest.Entry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			t.Fatalf("entry %q has type %q, want a regular file", hdr.Name, hdr.Typeflag)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading tar entry %q: %v", hdr.Name, err)
		}
		got = append(got, updatetest.Entry{Name: hdr.Name, Mode: fs.FileMode(hdr.Mode), Body: body})
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return got
}

// readZip returns every entry of a zip archive, failing t on a non-regular one.
func readZip(t *testing.T, data []byte) []updatetest.Entry {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	var got []updatetest.Entry
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			t.Fatalf("entry %q has mode %v, want a regular file", f.Name, f.Mode())
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening zip entry %q: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading zip entry %q: %v", f.Name, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("closing zip entry %q: %v", f.Name, err)
		}
		got = append(got, updatetest.Entry{Name: f.Name, Mode: f.Mode().Perm(), Body: body})
	}
	return got
}

func assertEntries(t *testing.T, got, want []updatetest.Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("archive holds %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Mode != want[i].Mode || !bytes.Equal(got[i].Body, want[i].Body) {
			t.Errorf("entry %d = {%q %v %q}, want {%q %v %q}",
				i, got[i].Name, got[i].Mode, got[i].Body, want[i].Name, want[i].Mode, want[i].Body)
		}
	}
}

func TestBuildArchiveRoundTrips(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ext  string
		read func(*testing.T, []byte) []updatetest.Entry
	}{
		{updatetest.ExtTarGz, readTarGz},
		{updatetest.ExtZip, readZip},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			t.Parallel()
			data, err := updatetest.BuildArchive(tc.ext, entries())
			if err != nil {
				t.Fatalf("BuildArchive: %v", err)
			}
			assertEntries(t, tc.read(t, data), entries())
		})
	}
}

func TestBuildArchiveIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{updatetest.ExtTarGz, updatetest.ExtZip} {
		first, err := updatetest.BuildArchive(ext, entries())
		if err != nil {
			t.Fatalf("BuildArchive(%q): %v", ext, err)
		}
		second, err := updatetest.BuildArchive(ext, entries())
		if err != nil {
			t.Fatalf("BuildArchive(%q): %v", ext, err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("BuildArchive(%q) built different bytes for the same entries", ext)
		}
	}
}

func TestBuildArchiveWithNoEntries(t *testing.T) {
	t.Parallel()
	tgz, err := updatetest.BuildArchive(updatetest.ExtTarGz, nil)
	if err != nil {
		t.Fatalf("BuildArchive(tar.gz): %v", err)
	}
	assertEntries(t, readTarGz(t, tgz), nil)
	z, err := updatetest.BuildArchive(updatetest.ExtZip, nil)
	if err != nil {
		t.Fatalf("BuildArchive(zip): %v", err)
	}
	assertEntries(t, readZip(t, z), nil)
}

func TestBuildArchiveRejectsUnknownExt(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{"", "tar.xz", "tgz", ".zip", "ZIP"} {
		data, err := updatetest.BuildArchive(ext, entries())
		if !errors.Is(err, updatetest.ErrUnsupportedExt) {
			t.Errorf("BuildArchive(%q) error = %v, want ErrUnsupportedExt", ext, err)
		}
		if data != nil {
			t.Errorf("BuildArchive(%q) returned %d bytes with its error", ext, len(data))
		}
	}
}

func TestExtTarGzIsTheUpdaterDefault(t *testing.T) {
	t.Parallel()
	if updatetest.ExtTarGz != asset.DefaultExt {
		t.Errorf("ExtTarGz = %q, want asset.DefaultExt %q", updatetest.ExtTarGz, asset.DefaultExt)
	}
}

func ExampleBuildArchive() {
	data, err := updatetest.BuildArchive(updatetest.ExtZip, []updatetest.Entry{
		{Name: "tool", Mode: 0o755, Body: []byte("binary")},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, f := range zr.File {
		fmt.Println(f.Name, f.Mode())
	}
	// Output:
	// tool -rwxr-xr-x
}
