package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	t.Helper()
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.DailySessionTarget != 0 {
		t.Errorf("default DailySessionTarget should be 0, got %d", cfg.DailySessionTarget)
	}
}

func TestLoadValid(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("daily_session_target = 6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DailySessionTarget != 6 {
		t.Errorf("want DailySessionTarget=6, got %d", cfg.DailySessionTarget)
	}
}

func TestLoadMalformed(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("not valid toml [[[\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed TOML, got nil")
	}
}

func TestLoadZeroTarget(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// Explicit zero is the same as the default.
	if err := os.WriteFile(path, []byte("daily_session_target = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DailySessionTarget != 0 {
		t.Errorf("want 0, got %d", cfg.DailySessionTarget)
	}
}
