package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config holds all user-editable settings from config.toml.
// Zero values are the defaults; a missing config file is not an error.
type Config struct {
	// DailySessionTarget is the number of sessions the user wants to complete
	// each day. Zero means no target (nothing is shown in the TUI).
	DailySessionTarget int `toml:"daily_session_target"`
}

// Load reads the config file at the given path and returns the decoded Config.
// A missing file returns the default Config (no error). A file that exists but
// cannot be parsed is a fatal error — it is printed to stderr and Load returns
// a non-nil error; callers should exit non-zero.
func Load(path string) (Config, error) {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}
