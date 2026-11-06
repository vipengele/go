package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.vipengele.org/cli/state"
)

// stateFileName is the name of the file a [fileStore] keeps its [State] in.
const stateFileName = "update-state.json"

// State is what an update check remembers between runs.
//
// It is stored as JSON with the keys "last_attempt", "latest_version" and "latest_channel", each
// omitted when zero. A reader ignores unknown keys, so a field added later stays readable by this
// one, and a missing key reads as the field's zero value, so a file without "latest_channel" holds
// a version learnt on Stable.
type State struct {
	// LastAttempt is when the last check was made, whether or not it succeeded. The zero time
	// means no check has been made.
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// LatestVersion is the newest release version the last successful check found, such as
	// "v1.2.3". Empty means none is known.
	LatestVersion string `json:"latest_version,omitzero"`
	// LatestChannel is the channel LatestVersion was learnt on, stored by its name ("beta",
	// "edge") through [Channel.MarshalText]. Stable is the zero value and is omitted.
	LatestChannel Channel `json:"latest_channel,omitzero"`
}

// lastAttemptOn returns LastAttempt when st was learnt on ch, and the zero time otherwise: an
// attempt on another channel says nothing about ch, so it never throttles a check of ch.
func (st State) lastAttemptOn(ch Channel) time.Time {
	if st.LatestChannel != ch {
		return time.Time{}
	}
	return st.LastAttempt
}

// latestVersionOn returns LatestVersion when it was learnt on ch, and empty otherwise: a version
// learnt on another channel is unknown to a follower of ch and is never announced to it.
func (st State) latestVersionOn(ch Channel) string {
	if st.LatestChannel != ch {
		return ""
	}
	return st.LatestVersion
}

// StateStore keeps a [State] between runs.
//
// Load and Save honour ctx: neither blocks past ctx being done. The background check of
// [Config.Start] calls both on its goroutine, and [Check.Finish] bounds the command's exit only
// while they return once it cancels them.
type StateStore interface {
	// Load returns the stored State. Nothing stored yet, or content that cannot be read as a
	// State, is the zero State and a nil error; an error means the store itself failed.
	Load(ctx context.Context) (State, error)
	// Save replaces the stored State with st.
	Save(ctx context.Context, st State) error
}

// fileStore is a [StateStore] over the file stateFileName in dir. It takes no lock: concurrent
// saves each replace the file whole, and the last rename wins.
//
// The file I/O has no cancellation of its own, so Load and Save run it on a goroutine and return
// ctx.Err() when ctx is done first. The goroutine finishes on its own and its result is discarded.
type fileStore struct {
	dir string

	// readFile and writeFile are the blocking I/O of Load and Save; nil means [fileStore.read] and
	// [fileStore.write]. Tests set them to stall the I/O.
	readFile  func() (State, error)
	writeFile func(State) error
}

var _ StateStore = (*fileStore)(nil)

// path returns the state file's path.
func (s *fileStore) path() string {
	return filepath.Join(s.dir, stateFileName)
}

// result is what an I/O goroutine hands back.
type result[T any] struct {
	val T
	err error
}

// within runs fn on a goroutine and returns its result, or ctx.Err() when ctx is done first. It
// does not run fn when ctx is already done. The channel holds the result, so an abandoned
// goroutine never blocks.
func within[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T

		return zero, err
	}

	ch := make(chan result[T], 1)

	go func() {
		v, err := fn()
		ch <- result[T]{val: v, err: err}
	}()

	select {
	case r := <-ch:
		return r.val, r.err
	case <-ctx.Done():
		var zero T

		return zero, ctx.Err()
	}
}

// Load implements [StateStore]. A missing file and a corrupt one are the zero State; a file that
// cannot be read is an error. It creates nothing. A done ctx returns ctx.Err() without reading,
// and a ctx done during the read returns ctx.Err() while the read finishes unobserved.
func (s *fileStore) Load(ctx context.Context) (State, error) {
	read := s.readFile
	if read == nil {
		read = s.read
	}

	return within(ctx, read)
}

// Save implements [StateStore]. It creates the directory with mode 0700 when it is missing and
// writes LastAttempt in UTC. A done ctx returns ctx.Err() without touching the disk. A ctx done
// during the write returns ctx.Err() while the write finishes or fails on its own; the write is
// atomic (temporary file, then rename), so a reader sees the old file or the new one, never a
// partial one.
func (s *fileStore) Save(ctx context.Context, st State) error {
	write := s.writeFile
	if write == nil {
		write = s.write
	}

	_, err := within(ctx, func() (struct{}, error) { return struct{}{}, write(st) })

	return err
}

func (s *fileStore) read() (State, error) {
	st, status, err := state.LoadJSON[State](s.path())
	if status == state.Unreadable {
		return State{}, fmt.Errorf("update: load state: %w", err)
	}

	return st, nil
}

func (s *fileStore) write(st State) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("update: save state: %w", err)
	}

	st.LastAttempt = st.LastAttempt.UTC()

	if err := state.SaveJSON(s.path(), st); err != nil {
		return fmt.Errorf("update: save state: %w", err)
	}

	return nil
}
