package updatetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// ExtTarGz is the extension of a gzip-compressed tar archive, the updater's default AssetExt.
const ExtTarGz = "tar.gz"

// ExtZip is the extension of a zip archive.
const ExtZip = "zip"

// ErrUnsupportedExt reports an archive extension [BuildArchive] has no format for.
var ErrUnsupportedExt = errors.New("unsupported archive extension")

// Entry is one regular file of an archive.
type Entry struct {
	// Name is the entry's path inside the archive, slash-separated, written as given.
	Name string
	// Mode holds the entry's permission bits, such as 0o755 for an executable. Only the
	// permission bits are written; a zero Mode is written as a file with no permissions.
	Mode fs.FileMode
	// Body is the entry's content.
	Body []byte
}

// BuildArchive returns entries written, in order, as an archive of the format ext names:
// [ExtTarGz] for a gzip-compressed tar archive, [ExtZip] for a zip archive. Any other ext fails
// with [ErrUnsupportedExt].
//
// Every entry carries the same fixed modification time, so equal entries build equal bytes.
func BuildArchive(ext string, entries []Entry) ([]byte, error) {
	switch ext {
	case ExtTarGz:
		return buildTarGz(entries)
	case ExtZip:
		return buildZip(entries)
	default:
		return nil, fmt.Errorf("updatetest: %w %q: want %q or %q", ErrUnsupportedExt, ext, ExtTarGz, ExtZip)
	}
}

// modTime is the modification time of every entry: the Unix epoch, which both formats store
// exactly.
func modTime() time.Time {
	return time.Unix(0, 0).UTC()
}

// buildTarGz writes entries as a tar archive inside a gzip stream with no header name or time.
func buildTarGz(entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     e.Name,
			Mode:     int64(e.Mode.Perm()),
			Size:     int64(len(e.Body)),
			ModTime:  modTime(),
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("updatetest: writing tar header %q: %w", e.Name, err)
		}
		if _, err := tw.Write(e.Body); err != nil {
			return nil, fmt.Errorf("updatetest: writing tar entry %q: %w", e.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("updatetest: closing tar archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("updatetest: closing gzip stream: %w", err)
	}
	return buf.Bytes(), nil
}

// buildZip writes entries as deflate-compressed zip entries, each with Mode as its file mode.
func buildZip(entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{
			Name:     e.Name,
			Method:   zip.Deflate,
			Modified: modTime(),
		}
		hdr.SetMode(e.Mode.Perm())
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("updatetest: writing zip header %q: %w", e.Name, err)
		}
		if _, err := w.Write(e.Body); err != nil {
			return nil, fmt.Errorf("updatetest: writing zip entry %q: %w", e.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("updatetest: closing zip archive: %w", err)
	}
	return buf.Bytes(), nil
}
