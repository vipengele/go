package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	want := State{
		LastAttempt:   time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC),
		LatestVersion: "v1.2.3",
	}
	store := &fileStore{dir: t.TempDir()}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastAttempt.Equal(want.LastAttempt) || got.LatestVersion != want.LatestVersion {
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
			if !got.LastAttempt.IsZero() || got.LatestVersion != "" {
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

func ptr[T any](v T) *T { return &v }
