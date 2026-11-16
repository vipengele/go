package selfupdate

import (
	"fmt"
	"time"
)

const (
	// DefaultInterval is the time between two update checks when Settings leaves Interval zero.
	DefaultInterval = 24 * time.Hour

	// MinInterval is the shortest Interval Settings accepts.
	MinInterval = time.Hour
)

// Settings are the configuration keys of a daemon's update checks, which a reload may change.
type Settings struct {
	// Interval is the time between two update checks. Zero means DefaultInterval; anything else
	// below MinInterval is invalid.
	Interval time.Duration `config:"interval" doc:"Time between two update checks, at least 1h."`

	// AutoApply makes a check that finds a newer release install it. Without it a check only
	// reports the release.
	AutoApply bool `config:"auto_apply" doc:"Install a newer release when a check finds one."`
}

// DefaultSettings returns the Settings a daemon uses when its configuration sets neither key: an
// Interval of DefaultInterval and AutoApply on.
func DefaultSettings() Settings {
	return Settings{Interval: DefaultInterval, AutoApply: true}
}

// Validate reports an Interval that is negative, or positive and below MinInterval. A zero
// Interval is valid and means DefaultInterval.
func (s Settings) Validate() error {
	if s.Interval != 0 && s.Interval < MinInterval {
		return fmt.Errorf("selfupdate: interval %v is below the minimum of %v", s.Interval, MinInterval)
	}
	return nil
}

// withDefaults returns s with a zero Interval replaced by DefaultInterval.
func (s Settings) withDefaults() Settings {
	if s.Interval == 0 {
		s.Interval = DefaultInterval
	}
	return s
}
