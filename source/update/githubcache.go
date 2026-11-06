package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.vipengele.org/cli/state"
)

// cacheFileName is the name of the file the cache [NewFileResponseCache] returns keeps its
// entries in.
const cacheFileName = "github-cache.json"

// ResponseCache keeps the last successful REST API response a [GitHubSource] received for each
// URL, so that the next request for it can be conditional.
//
// Get and Put honour ctx: neither blocks past ctx being done. A GitHubSource treats every failure
// of either as a miss or a skipped write, never as an error of Latest.
type ResponseCache interface {
	// Get returns the entry stored for url, and false when there is none or it cannot be read.
	Get(ctx context.Context, url string) (CachedResponse, bool)
	// Put replaces the entry stored for url with r.
	Put(ctx context.Context, url string, r CachedResponse) error
}

// CachedResponse is one entry of a [ResponseCache]: the ETag of a 200 response and its body.
type CachedResponse struct {
	// ETag is the response's ETag header, sent back as If-None-Match.
	ETag string `json:"etag"`
	// Body is the response body, decoded again when the server answers 304 Not Modified.
	Body []byte `json:"body"`
}

// NewFileResponseCache returns a [ResponseCache] over the file "github-cache.json" in dir, written
// through go.vipengele.org/cli/state. Put creates dir with mode 0700 when it is missing and
// replaces the file atomically, mode 0600; it takes no lock, so of two concurrent writers the last
// rename wins. A missing, unreadable or corrupt file is a miss. A done ctx makes Get a miss and Put
// return ctx.Err() without touching the disk; a ctx done during the I/O returns at once while the
// I/O finishes unobserved.
//
// The default Source of a [Config] uses this cache in its resolved StateDir. A [GitHubSource]
// built any other way has no cache unless its Cache field is set, for which this constructor is
// the way to share the default file.
func NewFileResponseCache(dir string) ResponseCache {
	return &fileCache{dir: dir}
}

// cacheFile is the content of the cache file: the entries keyed by URL.
type cacheFile struct {
	Entries map[string]CachedResponse `json:"entries"`
}

// fileCache is the [ResponseCache] NewFileResponseCache returns. Its file I/O has no cancellation
// of its own, so Get and Put run it through [within].
type fileCache struct {
	dir string

	// readFile and writeFile are the blocking I/O of Get and Put; nil means [fileCache.read] and
	// [fileCache.write]. Tests set them to stall the I/O.
	readFile  func() (cacheFile, error)
	writeFile func(cacheFile) error
}

var _ ResponseCache = (*fileCache)(nil)

// path returns the cache file's path.
func (c *fileCache) path() string {
	return filepath.Join(c.dir, cacheFileName)
}

// Get implements [ResponseCache].
func (c *fileCache) Get(ctx context.Context, url string) (CachedResponse, bool) {
	f, err := within(ctx, c.reader())
	if err != nil {
		return CachedResponse{}, false
	}

	r, ok := f.Entries[url]

	return r, ok
}

// Put implements [ResponseCache]. It reads the file, replaces the entry for url and writes the
// file whole; entries for other URLs are kept, and a file that cannot be read starts empty.
func (c *fileCache) Put(ctx context.Context, url string, r CachedResponse) error {
	read, write := c.reader(), c.writer()

	_, err := within(ctx, func() (struct{}, error) {
		f, err := read()
		if err != nil || f.Entries == nil {
			f = cacheFile{Entries: map[string]CachedResponse{}}
		}

		f.Entries[url] = r

		return struct{}{}, write(f)
	})

	return err
}

// reader returns readFile, or [fileCache.read] when it is nil.
func (c *fileCache) reader() func() (cacheFile, error) {
	if c.readFile != nil {
		return c.readFile
	}

	return c.read
}

// writer returns writeFile, or [fileCache.write] when it is nil.
func (c *fileCache) writer() func(cacheFile) error {
	if c.writeFile != nil {
		return c.writeFile
	}

	return c.write
}

// read loads the cache file. A missing or corrupt file is an empty one; an unreadable file is an
// error.
func (c *fileCache) read() (cacheFile, error) {
	f, status, err := state.LoadJSON[cacheFile](c.path())
	if status == state.Unreadable {
		return cacheFile{}, fmt.Errorf("update: load GitHub response cache: %w", err)
	}

	return f, nil
}

// write creates the directory with mode 0700 when it is missing and replaces the cache file with f.
func (c *fileCache) write(f cacheFile) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("update: save GitHub response cache: %w", err)
	}

	if err := state.SaveJSON(c.path(), f); err != nil {
		return fmt.Errorf("update: save GitHub response cache: %w", err)
	}

	return nil
}
