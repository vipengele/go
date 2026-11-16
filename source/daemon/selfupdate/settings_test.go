package selfupdate

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultSettingsCheckDailyAndApply(t *testing.T) {
	t.Parallel()

	got := DefaultSettings()
	if got.Interval != 24*time.Hour || !got.AutoApply {
		t.Errorf("DefaultSettings() = %+v, want Interval 24h and AutoApply true", got)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("DefaultSettings().Validate() = %v, want nil", err)
	}
}

func TestSettingsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		interval time.Duration
		valid    bool
	}{
		{interval: 0, valid: true},
		{interval: time.Hour, valid: true},
		{interval: 7 * 24 * time.Hour, valid: true},
		{interval: time.Hour - time.Nanosecond, valid: false},
		{interval: time.Minute, valid: false},
		{interval: -time.Hour, valid: false},
	}
	for _, tt := range tests {
		err := Settings{Interval: tt.interval}.Validate()
		if (err == nil) != tt.valid {
			t.Errorf("Settings{Interval: %v}.Validate() = %v, want valid %v", tt.interval, err, tt.valid)
		}
	}
}

func TestSettingsWithDefaultsFillsOnlyAZeroInterval(t *testing.T) {
	t.Parallel()

	if got := (Settings{}).withDefaults(); got.Interval != DefaultInterval || got.AutoApply {
		t.Errorf("Settings{}.withDefaults() = %+v, want Interval %v and AutoApply unchanged", got, DefaultInterval)
	}
	in := Settings{Interval: 2 * time.Hour, AutoApply: true}
	if got := in.withDefaults(); got != in {
		t.Errorf("withDefaults() = %+v, want %+v unchanged", got, in)
	}
}

func TestSettingsConfigKeys(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[Settings]()
	for field, key := range map[string]string{"Interval": "interval", "AutoApply": "auto_apply"} {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("Settings has no field %s", field)
		}
		if got := f.Tag.Get("config"); got != key {
			t.Errorf("Settings.%s config key = %q, want %q", field, got, key)
		}
	}
}
