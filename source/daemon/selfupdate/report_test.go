package selfupdate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/daemon"
)

// loadStore is a Store whose Load returns rec and err; Update must not be called.
type loadStore struct {
	rec Record
	err error
}

func (s loadStore) Load(context.Context) (Record, error) { return s.rec, s.err }

func (loadStore) Update(context.Context, func(Record) (Record, error)) error {
	panic("Reporter must not update the store")
}

func TestReporterMissingFileIsIdle(t *testing.T) {
	t.Parallel()

	got := Reporter(NewFileStore(recordPath(t)), nil)(t.Context())

	if got.State != daemon.UpdateIdle || got.Error != "" {
		t.Errorf("status = %+v, want idle without error", got)
	}
}

func TestReporterMapsStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   daemon.UpdateState
	}{
		{StatusIdle, daemon.UpdateIdle},
		{StatusPending, daemon.UpdatePending},
		{StatusConfirmed, daemon.UpdateConfirmed},
		{StatusRolledBack, daemon.UpdateRolledBack},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			t.Parallel()

			got := Reporter(loadStore{rec: Record{Status: tt.status}}, nil)(t.Context())
			if got.State != tt.want || got.Error != "" {
				t.Errorf("status = %+v, want %q without error", got, tt.want)
			}
		})
	}
}

func TestUpdateStateOfAnUnknownStatusIsUnknown(t *testing.T) {
	t.Parallel()

	if got := updateState(Status("upgrading")); got != daemon.UpdateUnknown {
		t.Errorf("updateState(upgrading) = %q, want %q", got, daemon.UpdateUnknown)
	}
}

func TestReporterCopiesRecordFields(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	rec := Record{
		Status:       StatusRolledBack,
		FromVersion:  "v1.0.0",
		ToVersion:    "v1.1.0",
		Target:       "/usr/local/bin/tool",
		Backup:       "/usr/local/bin/tool.old",
		BootAttempts: 3,
		Reason:       "boot attempts exhausted",
		StartedAt:    started,
		UpdatedAt:    started.Add(time.Minute),
	}

	got := Reporter(loadStore{rec: rec}, nil)(t.Context())
	want := daemon.UpdateStatus{
		State:        daemon.UpdateRolledBack,
		FromVersion:  "v1.0.0",
		ToVersion:    "v1.1.0",
		BootAttempts: 3,
		Reason:       "boot attempts exhausted",
		StartedAt:    started,
		UpdatedAt:    started.Add(time.Minute),
	}

	if got != want {
		t.Errorf("status = %+v, want %+v", got, want)
	}
}

func TestReporterCorruptFileIsUnknown(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	writeRaw(t, path, "{not json")

	got := Reporter(NewFileStore(path), nil)(t.Context())

	if got.State != daemon.UpdateUnknown || got.Error == "" {
		t.Errorf("status = %+v, want unknown with an error", got)
	}
}

func TestReporterStoreErrorIsUnknown(t *testing.T) {
	t.Parallel()

	got := Reporter(loadStore{err: errors.New("disk on fire")}, nil)(t.Context())

	if got.State != daemon.UpdateUnknown || !strings.Contains(got.Error, "disk on fire") {
		t.Errorf("status = %+v, want unknown carrying the store error", got)
	}
}

func TestReporterDoneContextIsUnknown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := Reporter(loadStore{rec: Record{Status: StatusPending}}, nil)(ctx)

	if got.State != daemon.UpdateUnknown || got.Error == "" {
		t.Errorf("status = %+v, want unknown with an error", got)
	}
}

func TestReporterCopiesReleaseInfo(t *testing.T) {
	t.Parallel()

	release := func(context.Context) (ReleaseInfo, error) {
		return ReleaseInfo{Channel: "stable", Watermark: "v1.2.0"}, nil
	}

	got := Reporter(loadStore{rec: Record{Status: StatusConfirmed}}, release)(t.Context())

	if got.State != daemon.UpdateConfirmed || got.Channel != "stable" || got.Watermark != "v1.2.0" || got.Error != "" {
		t.Errorf("status = %+v, want confirmed on stable at v1.2.0", got)
	}
}

func TestReporterReleaseErrorKeepsStatus(t *testing.T) {
	t.Parallel()

	release := func(context.Context) (ReleaseInfo, error) {
		return ReleaseInfo{Channel: "ignored"}, errors.New("no channel")
	}

	rec := Record{Status: StatusPending, ToVersion: "v1.1.0"}
	got := Reporter(loadStore{rec: rec}, release)(t.Context())

	if got.State != daemon.UpdatePending || got.ToVersion != "v1.1.0" {
		t.Errorf("status = %+v, want the pending record", got)
	}

	if !strings.Contains(got.Error, "no channel") || got.Channel != "" || got.Watermark != "" {
		t.Errorf("status = %+v, want the release error and no channel or watermark", got)
	}
}

func TestReporterNilReleaseReportsNoChannel(t *testing.T) {
	t.Parallel()

	got := Reporter(loadStore{rec: Record{Status: StatusPending}}, nil)(t.Context())

	if got.Channel != "" || got.Watermark != "" || got.Error != "" {
		t.Errorf("status = %+v, want no channel, watermark or error", got)
	}
}

func TestReporterLeavesStoreUntouched(t *testing.T) {
	t.Parallel()

	path := recordPath(t)
	store := NewFileStore(path)

	rec := Record{Status: StatusPending, FromVersion: "v1.0.0", ToVersion: "v1.1.0", BootAttempts: 1}
	if err := store.Update(t.Context(), put(rec)); err != nil {
		t.Fatal(err)
	}

	before := readRaw(t, path)

	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	got := Reporter(store, nil)(t.Context())
	if got.State != daemon.UpdatePending {
		t.Fatalf("status = %+v, want pending", got)
	}

	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if after := readRaw(t, path); after != before {
		t.Errorf("record changed:\n%s\nwant\n%s", after, before)
	}

	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Errorf("mtime = %v, want %v", infoAfter.ModTime(), infoBefore.ModTime())
	}
}
