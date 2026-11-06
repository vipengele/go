package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	want := State{
		LastAttempt:    time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC),
		LatestVersion:  "v1.2.3-beta.1",
		LatestChannel:  Beta,
		HighestApplied: "v1.2.0",
	}
	store := &fileStore{dir: t.TempDir()}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastAttempt.Equal(want.LastAttempt) || got.LatestVersion != want.LatestVersion ||
		got.LatestChannel != want.LatestChannel || got.HighestApplied != want.HighestApplied {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestFileStoreSaveWritesJSON(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("UTC+2", 2*60*60)

	tests := []struct {
		name string
		st   State
		want string
	}{
		{name: "zero", st: State{}, want: "{}\n"},
		{
			name: "both fields",
			st: State{
				LastAttempt:   time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC),
				LatestVersion: "v1.2.3",
			},
			want: "{\n  \"last_attempt\": \"2026-03-04T05:06:07.123456789Z\",\n  \"latest_version\": \"v1.2.3\"\n}\n",
		},
		{
			name: "edge channel",
			st:   State{LatestVersion: "v1.2.3-alpha.1", LatestChannel: Edge},
			want: "{\n  \"latest_version\": \"v1.2.3-alpha.1\",\n  \"latest_channel\": \"edge\"\n}\n",
		},
		{
			name: "stable channel is omitted",
			st:   State{LatestVersion: "v1.2.3", LatestChannel: Stable},
			want: "{\n  \"latest_version\": \"v1.2.3\"\n}\n",
		},
		{
			name: "highest applied",
			st:   State{LatestVersion: "v1.2.3", HighestApplied: "v1.2.0"},
			want: "{\n  \"latest_version\": \"v1.2.3\",\n  \"highest_applied\": \"v1.2.0\"\n}\n",
		},
		{
			name: "version only",
			st:   State{LatestVersion: "v1.2.3"},
			want: "{\n  \"latest_version\": \"v1.2.3\"\n}\n",
		},
		{
			name: "non-UTC attempt is written in UTC",
			st:   State{LastAttempt: time.Date(2026, 3, 4, 7, 6, 7, 0, zone)},
			want: "{\n  \"last_attempt\": \"2026-03-04T05:06:07Z\"\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fileStore{dir: t.TempDir()}
			if err := store.Save(context.Background(), tt.st); err != nil {
				t.Fatalf("Save: %v", err)
			}

			data, err := os.ReadFile(store.path())
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("file = %q, want %q", data, tt.want)
			}
		})
	}
}

func TestFileStoreLoadUnparseableIsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content *string
	}{
		{name: "missing"},
		{name: "empty", content: ptr("")},
		{name: "corrupt", content: ptr("{not json")},
		{name: "wrong type", content: ptr(`{"last_attempt": 5}`)},
		{name: "unknown channel", content: ptr(`{"latest_version":"v1.0.0","latest_channel":"nightly"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fileStore{dir: t.TempDir()}
			if tt.content != nil {
				if err := os.WriteFile(store.path(), []byte(*tt.content), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			got, err := store.Load(context.Background())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got != (State{}) {
				t.Errorf("Load = %+v, want the zero State", got)
			}
		})
	}
}

func TestFileStoreLoadIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	store := &fileStore{dir: t.TempDir()}
	content := `{"latest_version":"v2.0.0","future":true}`
	if err := os.WriteFile(store.path(), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LatestVersion != "v2.0.0" {
		t.Errorf("LatestVersion = %q, want v2.0.0", got.LatestVersion)
	}
}

func TestFileStoreLoadReadsTheChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    Channel
	}{
		{name: "no key is stable", content: `{"last_attempt":"2026-03-04T05:06:07Z","latest_version":"v2.0.0"}`, want: Stable},
		{name: "stable", content: `{"latest_version":"v2.0.0","latest_channel":"stable"}`, want: Stable},
		{name: "beta", content: `{"latest_version":"v2.0.0","latest_channel":"beta"}`, want: Beta},
		{name: "edge", content: `{"latest_version":"v2.0.0","latest_channel":"edge"}`, want: Edge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fileStore{dir: t.TempDir()}
			if err := os.WriteFile(store.path(), []byte(tt.content), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			got, err := store.Load(context.Background())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got.LatestVersion != "v2.0.0" || got.LatestChannel != tt.want {
				t.Errorf("Load = %+v, want v2.0.0 on %v", got, tt.want)
			}
		})
	}
}

func TestFileStoreLoadDirectoryInPlaceOfFile(t *testing.T) {
	t.Parallel()

	store := &fileStore{dir: t.TempDir()}
	if err := os.Mkdir(store.path(), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if _, err := store.Load(context.Background()); err == nil {
		t.Error("Load = nil error, want an error")
	}
}

func TestFileStoreSaveCreatesNestedDir(t *testing.T) {
	t.Parallel()

	store := &fileStore{dir: filepath.Join(t.TempDir(), "a", "b")}
	want := State{LatestVersion: "v1.0.0"}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LatestVersion != want.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", got.LatestVersion, want.LatestVersion)
	}
}

func TestFileStoreSaveFailsWhenDirCannotBeCreated(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := &fileStore{dir: filepath.Join(blocker, "sub")}

	if err := store.Save(context.Background(), State{}); err == nil {
		t.Error("Save = nil error, want an error")
	}
}

func TestFileStoreSaveFailsWhenStateFileCannotBeReplaced(t *testing.T) {
	t.Parallel()

	store := &fileStore{dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(store.path(), "child"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := store.Save(context.Background(), State{}); err == nil {
		t.Error("Save = nil error, want an error")
	}
}

func TestFileStoreLoadCreatesNothing(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "missing")
	store := &fileStore{dir: dir}

	if _, err := store.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) = %v, want not exist", dir, err)
	}
}

func TestFileStoreDoneContextTouchesNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := filepath.Join(t.TempDir(), "state")
	store := &fileStore{dir: dir}

	if _, err := store.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Load error = %v, want context.Canceled", err)
	}
	if err := store.Save(ctx, State{LatestVersion: "v1.0.0"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Save error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) = %v, want not exist", dir, err)
	}
}

func TestFileStoreDoneContextLeavesFileUnmodified(t *testing.T) {
	t.Parallel()

	store := &fileStore{dir: t.TempDir()}
	if err := store.Save(context.Background(), State{LatestVersion: "v1.0.0"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.Save(ctx, State{LatestVersion: "v2.0.0"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save error = %v, want context.Canceled", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LatestVersion != "v1.0.0" {
		t.Errorf("LatestVersion = %q, want v1.0.0", got.LatestVersion)
	}
}

func TestFileStoreReturnsWhenContextIsCancelledDuringIO(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	store := &fileStore{
		dir: t.TempDir(),
		readFile: func() (State, error) {
			started <- struct{}{}
			<-release

			return State{}, nil
		},
		writeFile: func(State) error {
			started <- struct{}{}
			<-release

			return nil
		},
	}

	calls := map[string]func(context.Context) error{
		"Load": func(ctx context.Context) error { _, err := store.Load(ctx); return err },
		"Save": func(ctx context.Context) error { return store.Save(ctx, State{}) },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan error, 1)
			go func() { done <- call(ctx) }()

			<-started
			cancel()

			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("call did not return after cancellation")
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestStateReadsOnlyItsOwnChannel(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	st := State{LastAttempt: at, LatestVersion: "v1.3.0-beta.1", LatestChannel: Beta}

	if got := st.latestVersionOn(Beta); got != "v1.3.0-beta.1" {
		t.Errorf("latestVersionOn(Beta) = %q, want v1.3.0-beta.1", got)
	}
	if got := st.lastAttemptOn(Beta); !got.Equal(at) {
		t.Errorf("lastAttemptOn(Beta) = %v, want %v", got, at)
	}
	for _, ch := range []Channel{Stable, Edge} {
		if got := st.latestVersionOn(ch); got != "" {
			t.Errorf("latestVersionOn(%v) = %q, want empty", ch, got)
		}
		if got := st.lastAttemptOn(ch); !got.IsZero() {
			t.Errorf("lastAttemptOn(%v) = %v, want the zero time", ch, got)
		}
	}
}
