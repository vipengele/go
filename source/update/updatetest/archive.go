package updatetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
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
// with [ErrUnsupportedExt]. An entry the format cannot hold, such as a tar entry whose name holds
// a NUL byte or a zip entry whose name ends in "/" and that has a Body, fails with an error naming
// it, and no bytes.
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

// buildTarGz returns entries written as a tar archive inside a gzip stream with no header name or
// time.
func buildTarGz(entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeTarGz(&buf, entries); err != nil {
		return nil, fmt.Errorf("updatetest: %w", err)
	}
	return buf.Bytes(), nil
}

// writeTarGz writes entries to w as a gzip-compressed tar archive, trailers included.
func writeTarGz(w io.Writer, entries []Entry) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := writeTarEntry(tw, e); err != nil {
			return fmt.Errorf("writing tar entry %q: %w", e.Name, err)
		}
	}
	return errors.Join(tw.Close(), gz.Close())
}

// writeTarEntry writes e as a regular file of tw in the PAX format.
func writeTarEntry(tw *tar.Writer, e Entry) error {
	err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     e.Name,
		Mode:     int64(e.Mode.Perm()),
		Size:     int64(len(e.Body)),
		ModTime:  modTime(),
		Format:   tar.FormatPAX,
	})
	if err == nil {
		_, err = tw.Write(e.Body)
	}
	return err
}

// buildZip returns entries written as deflate-compressed zip entries, each with Mode as its file
// mode.
func buildZip(entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeZip(&buf, entries); err != nil {
		return nil, fmt.Errorf("updatetest: %w", err)
	}
	return buf.Bytes(), nil
}

// writeZip writes entries to w as a zip archive, central directory included.
func writeZip(w io.Writer, entries []Entry) error {
	zw := zip.NewWriter(w)
	for _, e := range entries {
		if err := writeZipEntry(zw, e); err != nil {
			return fmt.Errorf("writing zip entry %q: %w", e.Name, err)
		}
	}
	return zw.Close()
}

// writeZipEntry writes e as a deflate-compressed entry of zw with Mode as its file mode.
func writeZipEntry(zw *zip.Writer, e Entry) error {
	hdr := &zip.FileHeader{
		Name:     e.Name,
		Method:   zip.Deflate,
		Modified: modTime(),
	}
	hdr.SetMode(e.Mode.Perm())
	w, err := zw.CreateHeader(hdr)
	if err == nil {
		_, err = w.Write(e.Body)
	}
	return err
}
