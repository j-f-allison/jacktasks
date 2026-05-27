package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds all user-editable settings from config.toml.
// Zero values are the defaults; a missing config file is not an error.
type Config struct {
	// DailySessionTarget is the number of sessions the user wants to complete
	// each day. Zero means no target (nothing is shown in the TUI).
	DailySessionTarget int `toml:"daily_session_target"`

	// Timezone is an IANA name (e.g. "America/Denver") used to bucket sessions
	// into days/weeks for Dailies/Weeklies and to display times. Empty means
	// the machine's local timezone. Sessions are always *stored* in UTC epoch
	// seconds; this only affects display and period boundaries.
	Timezone string `toml:"timezone"`

	// Location is the resolved *time.Location for Timezone, set by Load.
	// Always non-nil after a successful Load (defaults to time.Local).
	Location *time.Location `toml:"-"`
}

// Load reads the config file at the given path and returns the decoded Config.
// A missing file returns the default Config (no error). A file that exists but
// cannot be parsed — or that names an invalid timezone — is a fatal error: it
// is returned as a non-nil error; callers should print it and exit non-zero.
func Load(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.Location = time.Local
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg.Location = time.Local
	if cfg.Timezone != "" {
		loc, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return cfg, fmt.Errorf("config: invalid timezone %q: %w", cfg.Timezone, err)
		}
		cfg.Location = loc
	}
	return cfg, nil
}
