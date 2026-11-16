package selfupdate

import (
	"context"
	"fmt"

	"go.vipengele.org/cli/state"
	"go.vipengele.org/filelock"
)

// Store keeps a Record across processes. The steps of an update can run in different processes,
// so every change goes through Update, which reads, changes and writes the record under a lock.
type Store interface {
	// Load returns the stored Record. Nothing stored is the zero Record, which is idle, with a nil
	// error. A record that cannot be read or does not decode is an error, never idle.
	Load(ctx context.Context) (Record, error)

	// Update loads the Record, passes it to fn and stores what fn returns, holding a lock that
	// excludes every other Update on the same store from the load to the store. When the load
	// fails, fn is not called. When fn returns an error, nothing is stored and Update returns that
	// error.
	Update(ctx context.Context, fn func(Record) (Record, error)) error
}

// FileStore is a Store over one JSON file, written through go.vipengele.org/cli/state so that a
// reader sees the old record or the new one, never a partial write.
//
// Update locks the file <path>.lock with go.vipengele.org/filelock, which serialises Updates
// across processes and across FileStores on the same path within one process. Load takes no lock.
//
// The store creates no directory: the directory of path must exist. A missing file is idle. A
// file that does not decode, an empty one included, is an error that matches state.ErrCorrupt,
// and it stays an error until an operator deletes the file, which returns the store to idle. A
// corrupt record never reads as idle, because a pending update read as idle is never rolled back.
//
// The file I/O has no cancellation of its own, so it runs on a goroutine and Load and Update
// return ctx.Err() when ctx is done first. An abandoned read or write finishes on its own, and
// Update's lock is held until it does, so no other Update reads past a write still in flight.
type FileStore struct {
	path string
	lock filelock.Locker

	// readFile and writeFile are the blocking I/O of Load and Update; nil means [FileStore.read]
	// and [FileStore.write]. Tests set them to stall the I/O.
	readFile  func() (Record, error)
	writeFile func(Record) error
}

var _ Store = (*FileStore)(nil)

// NewFileStore returns a FileStore over the file at path. It touches nothing: the lock file
// <path>.lock is created by the first Update.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path, lock: filelock.New(path + ".lock")}
}

// result is what an I/O goroutine hands back.
type result[T any] struct {
	val T
	err error
}

// Load implements Store. A done ctx returns ctx.Err() without reading, and a ctx done during the
// read returns ctx.Err() while the read finishes unobserved.
func (s *FileStore) Load(ctx context.Context) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}

	read := s.reader()
	ch := make(chan result[Record], 1)

	go func() {
		rec, err := read()
		ch <- result[Record]{val: rec, err: err}
	}()

	select {
	case r := <-ch:
		return r.val, r.err
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
}

// Update implements Store. It writes StartedAt and UpdatedAt in UTC. A done ctx returns an error matching ctx.Err() without taking the lock.
// fn runs on the caller's goroutine. A ctx done before the write starts writes nothing; a ctx done
// during the write returns ctx.Err() while the write finishes or fails on its own.
func (s *FileStore) Update(ctx context.Context, fn func(Record) (Record, error)) error {
	unlock, err := s.lock.Lock(ctx)
	if err != nil {
		return fmt.Errorf("selfupdate: lock %s: %w", s.path, err)
	}

	read, write := s.reader(), s.writer()

	// The goroutine owns the lock from here on and releases it once its last I/O is over. It
	// writes the record sent on next, and writes nothing when next carries nil.
	loaded := make(chan result[Record], 1)
	next := make(chan *Record, 1)
	written := make(chan error, 1)

	go func() {
		defer unlock()

		rec, err := read()
		loaded <- result[Record]{val: rec, err: err}

		if err != nil {
			return
		}

		if r := <-next; r != nil {
			written <- write(*r)
		}
	}()

	// Exactly one value goes on next, whichever way Update returns, a panic in fn included, so
	// the goroutine never waits on it with the lock held.
	sent := false
	send := func(rec *Record) {
		if !sent {
			sent = true
			next <- rec
		}
	}
	defer send(nil)

	var r result[Record]

	select {
	case r = <-loaded:
	case <-ctx.Done():
		return ctx.Err()
	}

	if r.err != nil {
		return r.err
	}

	rec, err := fn(r.val)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	send(&rec)

	select {
	case err := <-written:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *FileStore) reader() func() (Record, error) {
	if s.readFile != nil {
		return s.readFile
	}

	return s.read
}

func (s *FileStore) writer() func(Record) error {
	if s.writeFile != nil {
		return s.writeFile
	}

	return s.write
}

func (s *FileStore) read() (Record, error) {
	rec, _, err := state.LoadJSON[Record](s.path)
	if err != nil {
		return Record{}, fmt.Errorf("selfupdate: load record: %w", err)
	}

	return rec, nil
}

func (s *FileStore) write(rec Record) error {
	rec.StartedAt = rec.StartedAt.UTC()
	rec.UpdatedAt = rec.UpdatedAt.UTC()

	if err := state.SaveJSON(s.path, rec); err != nil {
		return fmt.Errorf("selfupdate: save record: %w", err)
	}

	return nil
}
