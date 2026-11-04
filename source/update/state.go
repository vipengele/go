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
// It is stored as JSON with the keys "last_attempt" and "latest_version", each omitted when zero.
// A reader ignores unknown keys, so a field added later stays readable by this one.
type State struct {
	// LastAttempt is when the last check was made, whether or not it succeeded. The zero time
	// means no check has been made.
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// LatestVersion is the newest release version the last successful check found, such as
	// "v1.2.3". Empty means none is known.
	LatestVersion string `json:"latest_version,omitzero"`
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
type fileStore struct {
	dir string
}

var _ StateStore = (*fileStore)(nil)

// path returns the state file's path.
func (s *fileStore) path() string {
	return filepath.Join(s.dir, stateFileName)
}

// Load implements [StateStore]. A missing file and a corrupt one are the zero State; a file that
// cannot be read is an error. It creates nothing and ignores ctx.
func (s *fileStore) Load(context.Context) (State, error) {
	st, status, err := state.LoadJSON[State](s.path())
	if status == state.Unreadable {
		return State{}, fmt.Errorf("update: load state: %w", err)
	}

	return st, nil
}

// Save implements [StateStore]. It creates the directory with mode 0700 when it is missing and
// writes LastAttempt in UTC. It ignores ctx.
func (s *fileStore) Save(_ context.Context, st State) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("update: save state: %w", err)
	}

	st.LastAttempt = st.LastAttempt.UTC()

	if err := state.SaveJSON(s.path(), st); err != nil {
		return fmt.Errorf("update: save state: %w", err)
	}

	return nil
}
