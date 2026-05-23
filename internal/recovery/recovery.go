// Package recovery provides crash-recovery persistence for in-flight sessions.
// Write serializes a Sentinel to active.json atomically; Read deserializes it;
// Clear removes it. All three are idempotent and best-effort from the caller's
// perspective — errors are logged but never fatal.
package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	sentinelVersion = 1
	filename        = "active.json"
)

// PauseRecord is one completed pause interval stored as Unix epoch seconds.
type PauseRecord struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// CaptureRecord is one upn capture stored as Unix epoch seconds.
type CaptureRecord struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	CapturedAt int64  `json:"captured_at"`
}

// Sentinel is the snapshot written to active.json while a session is active or
// paused. Names are denormalized so the recover prompt renders without DB reads.
type Sentinel struct {
	Version            int             `json:"version"`
	SessionID          string          `json:"session_id"`
	ProjectID          string          `json:"project_id"`
	ProjectName        string          `json:"project_name"`
	CategoryID         string          `json:"category_id"`
	CategoryName       string          `json:"category_name"`
	PlannedDurationMin int             `json:"planned_duration_min"`
	StartedAt          int64           `json:"started_at"`
	TargetEndAt        int64           `json:"target_end_at"`
	Pauses             []PauseRecord   `json:"pauses"`
	CurrentPauseStart  int64           `json:"current_pause_start,omitempty"`
	Captures           []CaptureRecord `json:"captures"`
	State              string          `json:"state"` // "active" or "paused"
	WrittenAt          int64           `json:"written_at"`
}

func sentinelPath(dir string) string { return filepath.Join(dir, filename) }

// Write atomically serializes s to dir/active.json via a temp file + rename.
func Write(dir string, s Sentinel) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("recovery: marshal: %w", err)
	}
	tmp := sentinelPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("recovery: write tmp: %w", err)
	}
	if err := os.Rename(tmp, sentinelPath(dir)); err != nil {
		return fmt.Errorf("recovery: rename: %w", err)
	}
	return nil
}

// Read reads dir/active.json and returns the Sentinel. Returns nil, nil if the
// file does not exist. Returns an error if the file exists but is malformed.
func Read(dir string) (*Sentinel, error) {
	data, err := os.ReadFile(sentinelPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recovery: read: %w", err)
	}
	var s Sentinel
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("recovery: unmarshal: %w", err)
	}
	return &s, nil
}

// Clear removes dir/active.json. No-op if already absent.
func Clear(dir string) error {
	err := os.Remove(sentinelPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recovery: clear: %w", err)
	}
	return nil
}
