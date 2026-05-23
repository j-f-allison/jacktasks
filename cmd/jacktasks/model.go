package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/j-f-allison/jacktasks/internal/session"
	"github.com/j-f-allison/jacktasks/internal/store"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	boldStyle = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
)

// ── messages ──────────────────────────────────────────────────────────────────

type tickMsg time.Time

type categoriesLoadedMsg struct {
	cats []store.Category
	err  error
}

type projectsLoadedMsg struct {
	projs []store.Project
	err   error
}

type sessionSavedMsg struct{ id string }

type fatalMsg struct{ err error }

// ── UI sub-states ─────────────────────────────────────────────────────────────

// uiExtra covers UI modes that don't map 1:1 to session.Machine states.
type uiExtra int

const (
	uiExtraNone        uiExtra = iota
	uiExtraNewName             // entering a new category or project name
	uiExtraContinueDur         // entering duration for "continue session" (from WhatNext)
)

// ── resume candidate ──────────────────────────────────────────────────────────

type resumeInfo struct {
	categoryID   string
	projectID    string
	categoryName string
	projectName  string
	remaining    int
}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	store    *store.Store
	deviceID string
	ctx      context.Context
	machine  *session.Machine

	// nil once answered or if no candidate
	resume *resumeInfo

	// loaded for setup screens
	categories []store.Category
	projects   []store.Project

	extra            uiExtra
	selectedCatName  string
	selectedProjName string

	// single input component, reconfigured per screen
	input textinput.Model

	// updated by tick; used for countdown rendering
	now time.Time

	// when the 5-min break ends
	breakEnd time.Time

	// non-fatal inline error
	errMsg string

	// terminal size
	width  int
	height int

	// set before tea.Quit on a store failure
	fatalErr error
}

func newModel(s *store.Store, deviceID string, ctx context.Context) Model {
	ti := textinput.New()
	ti.Focus()

	m := Model{
		store:    s,
		deviceID: deviceID,
		ctx:      ctx,
		machine:  &session.Machine{},
		input:    ti,
		now:      time.Now(),
	}

	// Synchronous: local SQLite makes this fast enough to do at startup.
	m.resume = checkResume(ctx, s)

	// If no resume to offer, transition machine immediately so Init can
	// dispatch the categories load.
	if m.resume == nil {
		_ = m.machine.BeginSetup()
	}

	return m
}

func checkResume(ctx context.Context, s *store.Store) *resumeInfo {
	latest, err := s.LatestSession(ctx)
	if err != nil || latest.Status != store.SessionEndedEarly {
		return nil
	}
	remaining := latest.PlannedDurationMin - (latest.ActualDurationSec / 60)
	if remaining <= 0 {
		return nil
	}
	cat, err := s.GetCategory(ctx, latest.CategoryID)
	if err != nil {
		return nil
	}
	proj, err := s.GetProject(ctx, latest.ProjectID)
	if err != nil {
		return nil
	}
	return &resumeInfo{
		categoryID:   latest.CategoryID,
		projectID:    latest.ProjectID,
		categoryName: cat.Name,
		projectName:  proj.Name,
		remaining:    remaining,
	}
}

// ── tea.Model ─────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), textinput.Blink}
	if m.machine.State() == session.StateSetupCategory {
		cmds = append(cmds, m.loadCategoriesCmd())
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case fatalMsg:
		m.fatalErr = msg.err
		return m, tea.Quit

	case tickMsg:
		m.now = time.Time(msg)
		machState := m.machine.State()
		// Auto-end session when timer expires.
		if machState == session.StateActive && m.machine.TimeRemaining(m.now) == 0 {
			_ = m.machine.End(m.now)
			m.input.Reset()
			m.input.Placeholder = "notes"
			return m, tickCmd()
		}
		// Auto-end break after 5 minutes.
		if machState == session.StateBreak && !m.breakEnd.IsZero() && !m.now.Before(m.breakEnd) {
			_ = m.machine.EndBreak(m.now)
			m.breakEnd = time.Time{}
			m.input.Reset()
			m.input.Placeholder = "1-4"
			return m, tickCmd()
		}
		return m, tickCmd()

	case categoriesLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return fatalMsg{msg.err} }
		}
		m.categories = msg.cats
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, nil

	case projectsLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return fatalMsg{msg.err} }
		}
		m.projects = msg.projs
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, nil

	case sessionSavedMsg:
		m.input.Reset()
		m.input.Placeholder = "1-4"
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m.updateKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)

	if msg.Type != tea.KeyEnter {
		return m, tiCmd
	}

	val := strings.TrimSpace(m.input.Value())
	m.errMsg = ""

	// Resume offer lives before any machine state.
	if m.machine.State() == session.StateIdle && m.resume != nil {
		return m.handleResumeOffer(val)
	}

	switch m.machine.State() {
	case session.StateSetupCategory:
		return m.handleCategoryInput(val)
	case session.StateSetupProject:
		return m.handleProjectInput(val)
	case session.StateSetupDuration:
		return m.handleDurationInput(val)
	case session.StateActive, session.StatePaused:
		return m.handleActiveCommand(val)
	case session.StateEndingNotes:
		return m.handleEndingNotes(val)
	case session.StateWhatNext:
		return m.handleWhatNext(val)
	case session.StateBreak:
		_ = m.machine.EndBreak(m.now)
		m.breakEnd = time.Time{}
		m.input.Reset()
		m.input.Placeholder = "1-4"
		return m, tiCmd
	}

	return m, tiCmd
}

// ── per-state handlers ────────────────────────────────────────────────────────

func (m Model) handleResumeOffer(val string) (tea.Model, tea.Cmd) {
	if strings.ToLower(val) == "y" {
		ri := m.resume
		m.resume = nil
		m.selectedCatName = ri.categoryName
		m.selectedProjName = ri.projectName
		_ = m.machine.BeginSetup()
		_ = m.machine.SetCategory(ri.categoryID, m.now)
		_ = m.machine.SetProject(ri.projectID, m.now)
		_ = m.machine.SetDuration(ri.remaining, m.now)
		m.input.Reset()
		m.input.Placeholder = "command"
		return m, nil
	}
	m.resume = nil
	_ = m.machine.BeginSetup()
	m.input.Reset()
	return m, m.loadCategoriesCmd()
}

func (m Model) handleCategoryInput(val string) (tea.Model, tea.Cmd) {
	if m.extra == uiExtraNewName {
		if val == "" {
			m.errMsg = "name required"
			m.input.Reset()
			return m, nil
		}
		cat, err := m.store.CreateCategory(m.ctx, val)
		if err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.selectedCatName = cat.Name
		_ = m.machine.SetCategory(cat.ID, m.now)
		m.extra = uiExtraNone
		m.input.Reset()
		return m, m.loadProjectsCmd()
	}

	if strings.ToLower(val) == "n" {
		m.extra = uiExtraNewName
		m.input.Reset()
		m.input.Placeholder = "category name"
		return m, nil
	}

	n, err := strconv.Atoi(val)
	if err != nil || n < 1 || n > len(m.categories) {
		m.errMsg = "invalid choice"
		m.input.Reset()
		return m, nil
	}
	cat := m.categories[n-1]
	m.selectedCatName = cat.Name
	_ = m.machine.SetCategory(cat.ID, m.now)
	m.input.Reset()
	return m, m.loadProjectsCmd()
}

func (m Model) handleProjectInput(val string) (tea.Model, tea.Cmd) {
	if m.extra == uiExtraNewName {
		if val == "" {
			m.errMsg = "name required"
			m.input.Reset()
			return m, nil
		}
		proj, err := m.store.CreateProject(m.ctx, val, m.machine.CategoryID())
		if err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.selectedProjName = proj.Name
		_ = m.machine.SetProject(proj.ID, m.now)
		m.extra = uiExtraNone
		m.input.Reset()
		m.input.Placeholder = "minutes"
		return m, nil
	}

	if strings.ToLower(val) == "n" {
		m.extra = uiExtraNewName
		m.input.Reset()
		m.input.Placeholder = "project name"
		return m, nil
	}

	n, err := strconv.Atoi(val)
	if err != nil || n < 1 || n > len(m.projects) {
		m.errMsg = "invalid choice"
		m.input.Reset()
		return m, nil
	}
	proj := m.projects[n-1]
	m.selectedProjName = proj.Name
	_ = m.machine.SetProject(proj.ID, m.now)
	m.input.Reset()
	m.input.Placeholder = "minutes"
	return m, nil
}

func (m Model) handleDurationInput(val string) (tea.Model, tea.Cmd) {
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		m.errMsg = "enter a positive number"
		m.input.Reset()
		return m, nil
	}
	_ = m.machine.SetDuration(n, m.now)
	m.input.Reset()
	m.input.Placeholder = "command"
	return m, nil
}

func (m Model) handleActiveCommand(val string) (tea.Model, tea.Cmd) {
	if val == "" {
		return m, nil
	}
	parts := strings.SplitN(val, " ", 2)
	cmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	now := m.now

	switch cmd {
	case "upn":
		if arg == "" {
			m.errMsg = "usage: upn <text>"
		} else if err := m.machine.AddCapture(arg, now); err != nil {
			m.errMsg = err.Error()
		}
	case "ext":
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			m.errMsg = "usage: ext <minutes>"
		} else if err := m.machine.Extend(n, now); err != nil {
			m.errMsg = err.Error()
		}
	case "pause":
		if m.machine.State() == session.StatePaused {
			m.errMsg = "already paused — use resume"
		} else if err := m.machine.Pause(now); err != nil {
			m.errMsg = err.Error()
		}
	case "resume":
		if m.machine.State() == session.StateActive {
			m.errMsg = "not paused"
		} else if err := m.machine.Resume(now); err != nil {
			m.errMsg = err.Error()
		}
	case "end":
		if err := m.machine.End(now); err != nil {
			m.errMsg = err.Error()
		} else {
			m.input.Reset()
			m.input.Placeholder = "notes"
		}
	default:
		m.errMsg = fmt.Sprintf("unknown command %q", cmd)
	}

	m.input.Reset()
	return m, nil
}

func (m Model) handleEndingNotes(val string) (tea.Model, tea.Cmd) {
	_ = m.machine.SetEndNotes(val, m.now)
	return m, m.saveSessionCmd()
}

func (m Model) handleWhatNext(val string) (tea.Model, tea.Cmd) {
	if m.extra == uiExtraContinueDur {
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			m.errMsg = "enter a positive number"
			m.input.Reset()
			return m, nil
		}
		if err := m.machine.ContinueSession(n, m.now); err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.extra = uiExtraNone
		m.input.Reset()
		m.input.Placeholder = "command"
		return m, nil
	}

	now := m.now
	switch val {
	case "1":
		m.extra = uiExtraContinueDur
		m.input.Reset()
		m.input.Placeholder = "minutes"
	case "2":
		_ = m.machine.NewSession(now)
		m.extra = uiExtraNone
		m.selectedCatName = ""
		m.selectedProjName = ""
		m.input.Reset()
		return m, m.loadCategoriesCmd()
	case "3":
		_ = m.machine.StartBreak(now)
		m.breakEnd = now.Add(5 * time.Minute)
		m.input.Reset()
		m.input.Placeholder = "enter to end early"
	case "4":
		_ = m.machine.Finish(now)
		return m, tea.Quit
	default:
		m.errMsg = "enter 1, 2, 3, or 4"
		m.input.Reset()
	}
	return m, nil
}

// ── async commands ────────────────────────────────────────────────────────────

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadCategoriesCmd() tea.Cmd {
	ctx, s := m.ctx, m.store
	return func() tea.Msg {
		cats, err := s.ListCategories(ctx)
		return categoriesLoadedMsg{cats: cats, err: err}
	}
}

func (m Model) loadProjectsCmd() tea.Cmd {
	ctx, s, catID := m.ctx, m.store, m.machine.CategoryID()
	return func() tea.Msg {
		projs, err := s.ListProjectsByCategory(ctx, catID)
		return projectsLoadedMsg{projs: projs, err: err}
	}
}

func (m Model) saveSessionCmd() tea.Cmd {
	ctx := m.ctx
	s := m.store
	deviceID := m.deviceID
	// Snapshot before the goroutine runs to avoid machine-state races if the
	// user navigates WhatNext before the write completes.
	in, snapErr := m.machine.ToStoreSessionInput(deviceID)
	captures := m.machine.Captures()
	return func() tea.Msg {
		if snapErr != nil {
			return fatalMsg{snapErr}
		}
		sess, err := s.CreateSession(ctx, in)
		if err != nil {
			return fatalMsg{err}
		}
		for _, c := range captures {
			if _, err := s.CreateCapture(ctx, sess.ID, c.Text); err != nil {
				return fatalMsg{err}
			}
		}
		return sessionSavedMsg{id: sess.ID}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.fatalErr != nil {
		return fmt.Sprintf("error: %v\n", m.fatalErr)
	}

	var b strings.Builder
	b.WriteString(boldStyle.Render("jacktasks"))
	b.WriteString("\n\n")

	machState := m.machine.State()

	// Resume offer (machine is still Idle, waiting for user response).
	if machState == session.StateIdle && m.resume != nil {
		ri := m.resume
		fmt.Fprintf(&b, "Resume %s / %s with %d min remaining? [y/N]\n\n",
			ri.categoryName, ri.projectName, ri.remaining)
		b.WriteString(m.input.View())
		writeErr(&b, m.errMsg)
		return b.String()
	}

	switch machState {
	case session.StateSetupCategory:
		b.WriteString("Select category:\n\n")
		if m.extra == uiExtraNewName {
			b.WriteString("  New category name:\n\n")
		} else {
			for i, c := range m.categories {
				fmt.Fprintf(&b, "  %d) %s\n", i+1, c.Name)
			}
			b.WriteString("\n  n) New category\n\n")
		}

	case session.StateSetupProject:
		fmt.Fprintf(&b, "Select project (%s):\n\n", m.selectedCatName)
		if m.extra == uiExtraNewName {
			b.WriteString("  New project name:\n\n")
		} else {
			for i, p := range m.projects {
				fmt.Fprintf(&b, "  %d) %s\n", i+1, p.Name)
			}
			b.WriteString("\n  n) New project\n\n")
		}

	case session.StateSetupDuration:
		b.WriteString("Planned duration (minutes):\n\n")

	case session.StateActive:
		rem := m.machine.TimeRemaining(m.now)
		b.WriteString(boldStyle.Render(fmt.Sprintf("■ ACTIVE  %s", formatDuration(rem))))
		fmt.Fprintf(&b, "\n\n%s / %s\n\n", m.selectedCatName, m.selectedProjName)
		b.WriteString(dimStyle.Render("upn <text>  ext <n>  pause  end"))
		b.WriteString("\n\n")

	case session.StatePaused:
		rem := m.machine.TimeRemaining(m.now)
		b.WriteString(boldStyle.Render(fmt.Sprintf("⏸  PAUSED  %s", formatDuration(rem))))
		fmt.Fprintf(&b, "\n\n%s / %s\n\n", m.selectedCatName, m.selectedProjName)
		b.WriteString(dimStyle.Render("upn <text>  ext <n>  resume  end"))
		b.WriteString("\n\n")

	case session.StateEndingNotes:
		writeCaptureList(&b, m.machine.Captures())
		b.WriteString("End notes (Enter to skip):\n\n")

	case session.StateWhatNext:
		writeCaptureList(&b, m.machine.Captures())
		if m.extra == uiExtraContinueDur {
			b.WriteString("Duration for next session (minutes):\n\n")
		} else {
			b.WriteString("What next?\n\n")
			b.WriteString("  1) Continue session\n")
			b.WriteString("  2) New session\n")
			b.WriteString("  3) Break (5 min)\n")
			b.WriteString("  4) End\n\n")
		}

	case session.StateBreak:
		rem := m.breakEnd.Sub(m.now)
		if rem < 0 {
			rem = 0
		}
		b.WriteString(boldStyle.Render(fmt.Sprintf("Break  %s", formatDuration(rem))))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("Press Enter to end break early"))
		b.WriteString("\n\n")

	case session.StateIdle:
		return ""
	}

	b.WriteString(m.input.View())
	writeErr(&b, m.errMsg)
	return b.String()
}

// ── view helpers ──────────────────────────────────────────────────────────────

func writeCaptureList(b *strings.Builder, caps []session.Capture) {
	if len(caps) == 0 {
		return
	}
	b.WriteString("Captures:\n")
	for _, c := range caps {
		fmt.Fprintf(b, "  • %s\n", c.Text)
	}
	b.WriteString("\n")
}

func writeErr(b *strings.Builder, msg string) {
	if msg != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(msg))
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	total := int(d.Seconds())
	mins := total / 60
	secs := total % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}
