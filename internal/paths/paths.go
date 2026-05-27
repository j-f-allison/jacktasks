package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDir returns the directory where jacktasks stores its database and
// other persistent state. Creates the directory if it doesn't exist.
// On macOS this resolves to ~/Library/Application Support/jacktasks.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	dir := filepath.Join(base, "jacktasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	return dir, nil
}

// DBPath returns the absolute path to the SQLite database file.
func DBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jacktasks.db"), nil
}

// DBPathFromDir returns the SQLite path given an already-resolved data dir.
func DBPathFromDir(dir string) string {
	return filepath.Join(dir, "jacktasks.db")
}

// ConfigPath returns the absolute path to the user-editable TOML config file.
// The file may not exist; callers should treat a missing file as valid (defaults apply).
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "jacktasks", "config.toml"), nil
}