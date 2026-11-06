package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// downgradeConfig is a valid Config running version whose state store holds st.
func downgradeConfig(t *testing.T, running string, st State) Config {
	t.Helper()
	c := validConfig(t.TempDir())
	c.Version = running
	c.StateStore = &fakeStore{loads: []loadResult{{st: st}}}
	return c
}

func TestCheckDowngradeRefusesATagBelowTheFloor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		running   string
		highest   string
		tag       string
		wantFloor string
	}{
		{name: "below the running version, no watermark", running: "v1.3.0", tag: "v1.2.9", wantFloor: "v1.3.0"},
		{name: "watermark above the running version wins", running: "v1.2.0", highest: "v1.4.0", tag: "v1.3.0", wantFloor: "v1.4.0"},
		{name: "running version above the watermark wins", running: "v1.5.0", highest: "v1.4.0", tag: "v1.4.5", wantFloor: "v1.5.0"},
		{name: "prerelease of the floor", running: "v1.3.0", tag: "v1.3.0-beta.2", wantFloor: "v1.3.0"},
		{name: "watermark with a dev build", running: "dev", highest: "v1.3.0", tag: "v1.2.0", wantFloor: "v1.3.0"},
		{name: "tag not a release version", running: "v1.3.0", tag: "garbage", wantFloor: "v1.3.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := downgradeConfig(t, tc.running, State{HighestApplied: tc.highest})

			err := c.CheckDowngrade(context.Background(), tc.tag)

			var down *DowngradeError
			if !errors.As(err, &down) {
				t.Fatalf("CheckDowngrade(%q) = %v, want *DowngradeError", tc.tag, err)
			}
			want := DowngradeError{Name: "atk", Version: tc.tag, Floor: tc.wantFloor}
			if *down != want {
				t.Errorf("DowngradeError = %+v, want %+v", *down, want)
			}
		})
	}
}

func TestCheckDowngradeAllowsATagAtOrAboveTheFloor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		running string
		highest string
		tag     string
	}{
		{name: "equal to the running version", running: "v1.3.0", tag: "v1.3.0"},
		{name: "above the running version", running: "v1.3.0", tag: "v1.4.0"},
		{name: "equal to the watermark", running: "v1.2.0", highest: "v1.4.0", tag: "v1.4.0"},
		{name: "above the watermark", running: "v1.2.0", highest: "v1.4.0", tag: "v1.4.1"},
		{name: "release above its prerelease", running: "v1.3.0-beta.2", tag: "v1.3.0"},
		{name: "dev build with no watermark", running: "dev", tag: "v0.1.0"},
		{name: "no version with no watermark", running: "", tag: "v0.1.0"},
		{name: "watermark not a release version", running: "dev", highest: "garbage", tag: "v0.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := downgradeConfig(t, tc.running, State{HighestApplied: tc.highest})

			if err := c.CheckDowngrade(context.Background(), tc.tag); err != nil {
				t.Errorf("CheckDowngrade(%q) = %v, want nil", tc.tag, err)
			}
		})
	}
}

func TestCheckDowngradeReturnsTheStoresLoadError(t *testing.T) {
	t.Parallel()
	loadErr := errors.New("unreadable")
	c := validConfig(t.TempDir())
	c.Version = "v1.3.0"
	c.StateStore = &fakeStore{loads: []loadResult{{err: loadErr}}}

	err := c.CheckDowngrade(context.Background(), "v9.0.0")
	if !errors.Is(err, loadErr) {
		t.Errorf("CheckDowngrade() = %v, want the store's error", err)
	}
}

func TestCheckDowngradeFailsOnAnUnreadableStateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, stateFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	c := validConfig(t.TempDir())
	c.Version = "v1.3.0"
	c.StateDir = dir

	if err := c.CheckDowngrade(context.Background(), "v9.0.0"); err == nil {
		t.Error("CheckDowngrade() = nil, want the load error of an unreadable state file")
	}
}

func TestCheckDowngradeReadsTheWatermarkFromTheStateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := (&fileStore{dir: dir}).Save(context.Background(), State{HighestApplied: "v2.0.0"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c := validConfig(t.TempDir())
	c.Version = "v1.0.0"
	c.StateDir = dir

	var down *DowngradeError
	if err := c.CheckDowngrade(context.Background(), "v1.5.0"); !errors.As(err, &down) || down.Floor != "v2.0.0" {
		t.Errorf("CheckDowngrade() = %v, want *DowngradeError with Floor v2.0.0", err)
	}
}

func TestCheckDowngradeFallsBackToTheVersionWhenTheStateIsCorrupt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := validConfig(t.TempDir())
	c.Version = "v1.0.0"
	c.StateDir = dir

	if err := c.CheckDowngrade(context.Background(), "v1.0.0"); err != nil {
		t.Errorf("CheckDowngrade(v1.0.0) = %v, want nil", err)
	}
	var down *DowngradeError
	if err := c.CheckDowngrade(context.Background(), "v0.9.0"); !errors.As(err, &down) || down.Floor != "v1.0.0" {
		t.Errorf("CheckDowngrade(v0.9.0) = %v, want *DowngradeError with Floor v1.0.0", err)
	}
}

func TestCheckDowngradeReturnsAnInvalidConfigsError(t *testing.T) {
	t.Parallel()
	c := downgradeConfig(t, "v1.0.0", State{})
	c.TrustedKeys = nil

	err := c.CheckDowngrade(context.Background(), "v2.0.0")
	if err == nil {
		t.Fatal("CheckDowngrade() = nil, want the validation error")
	}
	var down *DowngradeError
	if errors.As(err, &down) {
		t.Errorf("CheckDowngrade() = %v, want the validation error, not a *DowngradeError", err)
	}
}
