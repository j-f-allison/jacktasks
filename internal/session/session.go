// Package session implements the in-memory session state machine.
// It has no I/O; all methods take an explicit now time.Time so they are
// testable with a fake clock. The caller writes to the store on session end.
package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/j-f-allison/jacktasks/internal/store"
)

// State represents the current phase of the session flow.
type State int

const (
	StateIdle         State = iota
	StateSetupCategory
	StateSetupProject
	StateSetupDuration
	StateActive
	StatePaused
	StateEndingNotes
	StateWhatNext
	StateBreak
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateSetupCategory:
		return "SetupCategory"
	case StateSetupProject:
		return "SetupProject"
	case StateSetupDuration:
		return "SetupDuration"
	case StateActive:
		return "Active"
	case StatePaused:
		return "Paused"
	case StateEndingNotes:
		return "EndingNotes"
	case StateWhatNext:
		return "WhatNext"
	case StateBreak:
		return "Break"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// pauseInterval records one pause/resume pair.
type pauseInterval struct {
	pausedAt  time.Time
	resumedAt time.Time // zero if still paused
}

// Capture is an upn thought captured during the session.
type Capture struct {
	ID         string
	Text       string
	CapturedAt time.Time
}

// Machine holds the in-memory state for one session flow. Zero value is
// valid with State == StateIdle.
type Machine struct {
	state State

	// setup fields, populated during SetupCategory/Project/Duration
	categoryID string
	projectID  string
	plannedMin int

	// active session fields
	sessionID  string
	startedAt  time.Time
	targetEnd  time.Time // shifts forward on pause resume
	pauses     []pauseInterval
	captures   []Capture

	// end fields, populated during EndingNotes
	endNotes string
	endedAt  time.Time
	status   store.SessionStatus

	// break tracking
	breakStart time.Time
}

var (
	ErrWrongState = errors.New("command not valid in current state")
)

// State returns the current state.
func (m *Machine) State() State { return m.state }

// Captures returns a copy of the captures recorded so far.
func (m *Machine) Captures() []Capture {
	out := make([]Capture, len(m.captures))
	copy(out, m.captures)
	return out
}

// TimeRemaining returns how much working time is left toward the planned
// duration. Valid during Active and Paused states. Returns zero otherwise.
func (m *Machine) TimeRemaining(now time.Time) time.Duration {
	if m.state != StateActive && m.state != StatePaused {
		return 0
	}
	if m.state == StatePaused {
		// target will shift on resume; return distance from now to adjusted target
		// treating current pause as not yet counted
		elapsed := m.pausedDuration(now)
		planned := time.Duration(m.plannedMin) * time.Minute
		actual := now.Sub(m.startedAt) - elapsed
		remaining := planned - actual
		if remaining < 0 {
			return 0
		}
		return remaining
	}
	remaining := m.targetEnd.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// pausedDuration sums all completed pause intervals plus any in-progress pause.
func (m *Machine) pausedDuration(now time.Time) time.Duration {
	var total time.Duration
	for _, p := range m.pauses {
		if p.resumedAt.IsZero() {
			total += now.Sub(p.pausedAt)
		} else {
			total += p.resumedAt.Sub(p.pausedAt)
		}
	}
	return total
}

// actualDurationSec computes (ended_at - started_at) - sum(pause intervals).
func (m *Machine) actualDurationSec() int {
	total := m.endedAt.Sub(m.startedAt) - m.pausedDuration(m.endedAt)
	if total < 0 {
		return 0
	}
	return int(total.Seconds())
}

// BeginSetup transitions from Idle to SetupCategory to start a new session
// flow. Called by the driver after any resume check.
func (m *Machine) BeginSetup() error {
	if m.state != StateIdle {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.state = StateSetupCategory
	return nil
}

// SetCategory sets the category and advances to SetupProject.
// Valid from Idle or SetupCategory.
func (m *Machine) SetCategory(categoryID string, now time.Time) error {
	if m.state != StateSetupCategory {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if categoryID == "" {
		return errors.New("categoryID required")
	}
	m.categoryID = categoryID
	m.state = StateSetupProject
	return nil
}

// SetProject sets the project and advances to SetupDuration.
// Valid from SetupProject.
func (m *Machine) SetProject(projectID string, now time.Time) error {
	if m.state != StateSetupProject {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if projectID == "" {
		return errors.New("projectID required")
	}
	m.projectID = projectID
	m.state = StateSetupDuration
	return nil
}

// SetDuration sets the planned duration and starts the session.
// Valid from SetupDuration.
func (m *Machine) SetDuration(minutes int, now time.Time) error {
	if m.state != StateSetupDuration {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if minutes <= 0 {
		return errors.New("planned duration must be positive")
	}
	m.plannedMin = minutes
	m.sessionID = uuid.NewString()
	m.startedAt = now
	m.targetEnd = now.Add(time.Duration(minutes) * time.Minute)
	m.pauses = m.pauses[:0]
	m.captures = m.captures[:0]
	m.state = StateActive
	return nil
}

// Pause pauses the timer. Valid from Active only.
func (m *Machine) Pause(now time.Time) error {
	if m.state != StateActive {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.pauses = append(m.pauses, pauseInterval{pausedAt: now})
	m.state = StatePaused
	return nil
}

// Resume resumes from Paused, shifting the target end time forward by the
// pause duration so the session still aims for the same working time.
func (m *Machine) Resume(now time.Time) error {
	if m.state != StatePaused {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	last := &m.pauses[len(m.pauses)-1]
	last.resumedAt = now
	m.targetEnd = m.targetEnd.Add(now.Sub(last.pausedAt))
	m.state = StateActive
	return nil
}

// AddCapture records an upn thought. Valid from Active or Paused.
func (m *Machine) AddCapture(text string, now time.Time) error {
	if m.state != StateActive && m.state != StatePaused {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if text == "" {
		return errors.New("capture text required")
	}
	m.captures = append(m.captures, Capture{
		ID:         uuid.NewString(),
		Text:       text,
		CapturedAt: now,
	})
	return nil
}

// Extend shifts the target end time forward by n minutes. Valid from Active
// or Paused. Does not affect actual_duration_sec or planned_duration_min.
func (m *Machine) Extend(minutes int, now time.Time) error {
	if m.state != StateActive && m.state != StatePaused {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if minutes <= 0 {
		return errors.New("extension must be positive")
	}
	m.targetEnd = m.targetEnd.Add(time.Duration(minutes) * time.Minute)
	return nil
}

// End transitions from Active or Paused to EndingNotes and records end time
// and status. If in Paused, the open pause interval is closed first.
func (m *Machine) End(now time.Time) error {
	if m.state != StateActive && m.state != StatePaused {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if m.state == StatePaused {
		last := &m.pauses[len(m.pauses)-1]
		last.resumedAt = now
	}
	m.endedAt = now

	actualSec := m.actualDurationSec()
	plannedSec := m.plannedMin * 60
	if actualSec >= plannedSec {
		m.status = store.SessionCompleted
	} else {
		m.status = store.SessionEndedEarly
	}
	m.state = StateEndingNotes
	return nil
}

// SetEndNotes records the end-of-session notes and advances to WhatNext.
// Valid from EndingNotes.
func (m *Machine) SetEndNotes(notes string, now time.Time) error {
	if m.state != StateEndingNotes {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.endNotes = notes
	m.state = StateWhatNext
	return nil
}

// StartBreak begins a 5-minute break from WhatNext.
func (m *Machine) StartBreak(now time.Time) error {
	if m.state != StateWhatNext {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.breakStart = now
	m.state = StateBreak
	return nil
}

// EndBreak returns from Break to WhatNext.
func (m *Machine) EndBreak(now time.Time) error {
	if m.state != StateBreak {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.state = StateWhatNext
	return nil
}

// ContinueSession starts a new session with the same category/project.
// Valid from WhatNext. Returns to Active state.
func (m *Machine) ContinueSession(minutes int, now time.Time) error {
	if m.state != StateWhatNext {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	if minutes <= 0 {
		return errors.New("planned duration must be positive")
	}
	m.plannedMin = minutes
	m.sessionID = uuid.NewString()
	m.startedAt = now
	m.targetEnd = now.Add(time.Duration(minutes) * time.Minute)
	m.pauses = m.pauses[:0]
	m.captures = m.captures[:0]
	m.endNotes = ""
	m.endedAt = time.Time{}
	m.state = StateActive
	return nil
}

// NewSession resets all session fields and returns to SetupCategory.
// Valid from WhatNext.
func (m *Machine) NewSession(now time.Time) error {
	if m.state != StateWhatNext {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	*m = Machine{state: StateSetupCategory}
	return nil
}

// Finish returns to Idle. Valid from WhatNext.
func (m *Machine) Finish(now time.Time) error {
	if m.state != StateWhatNext {
		return fmt.Errorf("%w: %s", ErrWrongState, m.state)
	}
	m.state = StateIdle
	return nil
}

// ToStoreSessionInput converts the completed session to the input struct
// for store.CreateSession. Only valid after End has been called (i.e.,
// state is EndingNotes or WhatNext).
func (m *Machine) ToStoreSessionInput(deviceID string) (store.CreateSessionInput, error) {
	if m.endedAt.IsZero() {
		return store.CreateSessionInput{}, errors.New("session has not ended")
	}
	return store.CreateSessionInput{
		CategoryID:         m.categoryID,
		ProjectID:          m.projectID,
		PlannedDurationMin: m.plannedMin,
		ActualDurationSec:  m.actualDurationSec(),
		StartedAt:          m.startedAt,
		EndedAt:            m.endedAt,
		EndNotes:           m.endNotes,
		Status:             m.status,
		DeviceID:           deviceID,
	}, nil
}

// SessionID returns the UUID assigned to the current session. Empty if no
// session has started yet.
func (m *Machine) SessionID() string { return m.sessionID }

// CategoryID returns the selected category ID.
func (m *Machine) CategoryID() string { return m.categoryID }

// ProjectID returns the selected project ID.
func (m *Machine) ProjectID() string { return m.projectID }

// PlannedMin returns the planned duration in minutes.
func (m *Machine) PlannedMin() int { return m.plannedMin }

// StartedAt returns when the current session started.
func (m *Machine) StartedAt() time.Time { return m.startedAt }

// Status returns the terminal status set by End.
func (m *Machine) Status() store.SessionStatus { return m.status }
