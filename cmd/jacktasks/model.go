package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/j-f-allison/jacktasks/internal/recovery"
	"github.com/j-f-allison/jacktasks/internal/reminders"
	"github.com/j-f-allison/jacktasks/internal/session"
	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncclient"
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

type inboxLoadedMsg struct {
	items []reminders.Reminder
	err   error
}

type captureActedMsg struct {
	captureID string
	err       error
}

type fatalMsg struct{ err error }

type syncDoneMsg struct {
	summary string
	err     error
	manual  bool // true if user-triggered via "s) Sync now"; false for auto-sync
}

type remListsLoadedMsg struct {
	lists []string
	err   error
}

type projRemindersLoadedMsg struct {
	items []reminders.Reminder
	err   error
}

// ── UI sub-states ─────────────────────────────────────────────────────────────

// uiExtra covers UI modes that don't map 1:1 to session.Machine states.
type uiExtra int

const (
	uiExtraNone          uiExtra = iota
	uiExtraNewName               // entering a new category or project name
	uiExtraContinueDur           // entering duration for "continue session" (from WhatNext)
	uiExtraStart                 // startup screen: inbox + resume + new session
	uiExtraRecover               // crash-recovery offer shown before the start screen
	uiExtraReminderDispo         // after end notes: confirm whether to mark the originating reminder complete
	uiExtraRemListPicker         // picker shown when user presses 'l' on a project to set its Reminders list
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
	dataDir  string
	ctx      context.Context
	machine  *session.Machine

	// nil if no resume candidate (ended_early session in DB)
	resume *resumeInfo

	// non-nil when a crash sentinel was found and not yet handled
	crashSentinel *recovery.Sentinel

	// Reminders integration; nil if EventKit unavailable
	remClient   reminders.Client
	inboxItems  []reminders.Reminder
	inboxLoaded bool

	// per-project reminders: loaded when a project with RemindersListName is selected
	selectedProjRemindersList string           // RemindersListName of the currently selected project
	projReminderItems         []reminders.Reminder // incomplete items from that list

	// list picker overlay ('l' on project screen)
	remListsEditIdx int      // index into m.projects of the project being edited
	remLists        []string // all available Reminders list names (nil = not yet loaded)

	// loaded for setup screens
	categories []store.Category
	projects   []store.Project

	extra            uiExtra
	selectedCatName  string
	selectedProjName string

	// capture context text shown during session setup from "Do" or inbox item
	doContextText string

	// when a session was started from an inbox reminder, completion is deferred
	// to the end of the session (asked on uiExtraReminderDispo). Cleared after
	// the disposition is resolved.
	pendingReminderID    string
	pendingReminderTitle string

	// notes value stashed while uiExtraReminderDispo is shown, so the save can
	// proceed after the user answers the disposition prompt.
	pendingEndNotes string

	// captures disposed on the current WhatNext screen (key = capture ID)
	capturesDisposed map[string]bool

	// single input component, reconfigured per screen
	input textinput.Model

	// multi-line word-wrapped input for end-of-session notes
	noteArea textarea.Model

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

	// Phase 5.5: polish components
	cursor       int             // keyboard cursor for list screens
	sp           spinner.Model   // for async-op indicators
	prog         progress.Model  // for timer progress bar
	helpModel    help.Model      // for footer key hints
	showFullHelp bool            // toggled by '?'
	savingSession bool           // true while session DB write is in flight
	flashOn       bool            // toggled by tick to flash the end-notes banner

	// sync (Phase 6c): config plumbed from env at launch; non-empty URL+Token
	// enables the "s) Sync now" menu option on the startup screen.
	syncCfg     syncclient.Config
	syncing     bool   // true while any sync (manual or auto) is in flight
	syncManual  bool   // true when the in-flight sync is user-triggered (affects rendering)
	syncSummary string // last sync result, shown on the start screen
	lastSyncAt  time.Time // when the last sync attempt completed; zero = never
	lastSyncOK  bool   // whether the last sync succeeded; only meaningful if !lastSyncAt.IsZero()
}

func newModel(s *store.Store, deviceID, dataDir string, ctx context.Context, remClient reminders.Client, syncCfg syncclient.Config) Model {
	ti := textinput.New()
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "optional notes..."
	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	ta.CharLimit = 0

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = StyleAccent

	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 40

	m := Model{
		store:     s,
		deviceID:  deviceID,
		dataDir:   dataDir,
		ctx:       ctx,
		machine:   &session.Machine{},
		input:     ti,
		noteArea:  ta,
		now:       time.Now(),
		remClient: remClient,
		sp:        sp,
		prog:      prog,
		helpModel: help.New(),
		syncCfg:   syncCfg,
	}

	// Check for a crash sentinel before anything else. Both reads are local and fast.
	if sentinel, err := recovery.Read(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "recovery read: %v\n", err)
	} else if sentinel != nil {
		_, dbErr := s.GetSession(ctx, sentinel.SessionID)
		if errors.Is(dbErr, store.ErrNotFound) {
			// Session was never written — this is a real crash. Offer recovery.
			m.crashSentinel = sentinel
			m.extra = uiExtraRecover
			m.input.Placeholder = "y/n"
			return m
		}
		// Session is in DB — previous run completed cleanly but crashed before Clear.
		_ = recovery.Clear(dataDir)
	}

	m.initStartup()
	// If sync is configured, the Init Cmd will fire an auto-sync; mark it
	// in-flight up front so a manual "s) Sync now" press doesn't race it.
	if m.syncCfg.URL != "" && m.syncCfg.Token != "" {
		m.syncing = true
	}
	return m
}

// initStartup sets up the resume candidate and start-screen state. Called on
// first launch and after the user discards a crash sentinel.
func (m *Model) initStartup() {
	m.resume = checkResume(m.ctx, m.store)
	m.cursor = 0
	if m.remClient != nil || m.resume != nil {
		m.extra = uiExtraStart
		m.input.Placeholder = "choice"
	} else {
		m.extra = uiExtraNone
		_ = m.machine.BeginSetup()
		m.input.Placeholder = "choice"
	}
}

func checkResume(ctx context.Context, s *store.Store) *resumeInfo {
	latest, err := s.LatestSession(ctx)
	if err != nil || latest.Status != store.SessionEndedEarly {
		return nil
	}
	remaining := latest.PlannedDurationMin - (latest.ActualDurationSec / 60)
	if remaining <= 5 {
		return nil
	}
	cat, err := s.GetCategory(ctx, latest.CategoryID)
	if err != nil {
		return nil
	}
	projName := ""
	if latest.ProjectID != "" {
		proj, err := s.GetProject(ctx, latest.ProjectID)
		if err != nil {
			return nil
		}
		projName = proj.Name
	}
	return &resumeInfo{
		categoryID:   latest.CategoryID,
		projectID:    latest.ProjectID,
		categoryName: cat.Name,
		projectName:  projName,
		remaining:    remaining,
	}
}

// ── tea.Model ─────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), textinput.Blink}
	if m.extra == uiExtraRecover {
		return tea.Batch(cmds...)
	}
	if m.machine.State() == session.StateSetupProject {
		cmds = append(cmds, m.loadProjectsCmd())
	}
	if m.remClient != nil && m.extra == uiExtraStart {
		cmds = append(cmds, m.loadInboxCmd(), m.sp.Tick)
	}
	// Fire a background sync on startup. Fire-and-forget — the UI never waits
	// on it and the menu/inbox keep rendering normally.
	if m.syncConfigured() {
		cmds = append(cmds, m.runSyncCmd(false))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.prog.Width = max(m.width-8, 20)
		m.helpModel.Width = m.width
		m.noteArea.SetWidth(max(m.width-4, 20))
		return m, nil

	case fatalMsg:
		m.fatalErr = msg.err
		return m, tea.Quit

	case progress.FrameMsg:
		prog, cmd := m.prog.Update(msg)
		m.prog = prog.(progress.Model)
		return m, cmd

	case spinner.TickMsg:
		if m.isLoading() {
			m.sp, _ = m.sp.Update(msg)
			return m, m.sp.Tick
		}
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		machState := m.machine.State()

		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd())

		// Update progress bar when in a timed state.
		if machState == session.StateActive || machState == session.StatePaused || machState == session.StateBreak {
			cmds = append(cmds, m.prog.SetPercent(m.timerPct()))
		}

		// Flash the end-notes banner while the user hasn't started typing.
		if machState == session.StateEndingNotes && m.noteArea.Value() == "" {
			m.flashOn = !m.flashOn
		} else {
			m.flashOn = false
		}

		// Auto-end session when timer expires.
		if machState == session.StateActive && m.machine.TimeRemaining(m.now) == 0 {
			_ = m.machine.End(m.now)
			m.enterEndingNotes()
			return m, tea.Batch(cmds...)
		}
		// Auto-end break after 5 minutes.
		if machState == session.StateBreak && !m.breakEnd.IsZero() && !m.now.Before(m.breakEnd) {
			_ = m.machine.EndBreak(m.now)
			m.breakEnd = time.Time{}
			m.input.Reset()
			m.input.Placeholder = "1-4"
			return m, tea.Batch(cmds...)
		}
		return m, tea.Batch(cmds...)

	case remListsLoadedMsg:
		if msg.err != nil {
			fmt.Fprintf(os.Stderr, "load reminder lists: %v\n", msg.err)
		}
		m.remLists = msg.lists
		return m, nil

	case projRemindersLoadedMsg:
		if msg.err != nil {
			fmt.Fprintf(os.Stderr, "load project reminders: %v\n", msg.err)
		} else {
			m.projReminderItems = msg.items
		}
		return m, nil

	case categoriesLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return fatalMsg{msg.err} }
		}
		m.categories = msg.cats
		m.cursor = 0
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, nil

	case projectsLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return fatalMsg{msg.err} }
		}
		m.projects = msg.projs
		m.cursor = 0
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, nil

	case sessionSavedMsg:
		m.savingSession = false
		m.capturesDisposed = make(map[string]bool)
		m.cursor = 0
		m.input.Reset()
		m.input.Placeholder = "choice"
		cmds := []tea.Cmd{m.clearSentinelCmd()}
		// Kick off a background sync now that the session is on disk. Skip
		// if another sync is already in flight — the next session-save (or
		// a manual trigger) will catch any drift.
		if m.syncConfigured() && !m.syncing {
			m.syncing = true
			cmds = append(cmds, m.runSyncCmd(false))
		}
		return m, tea.Batch(cmds...)

	case inboxLoadedMsg:
		m.inboxLoaded = true
		m.cursor = 0
		if msg.err == nil {
			m.inboxItems = msg.items
		}
		return m, nil

	case syncDoneMsg:
		m.syncing = false
		m.lastSyncAt = time.Now()
		m.lastSyncOK = msg.err == nil
		if msg.manual {
			m.syncManual = false
			m.cursor = 0
			m.input.Reset()
			m.input.Placeholder = "choice"
			m.syncSummary = msg.summary
			if msg.err != nil {
				m.errMsg = "sync error: " + msg.err.Error()
			}
		} else if m.extra == uiExtraStart {
			// Background sync completed while user is still on the start
			// screen: update the displayed summary so they see fresh state.
			m.syncSummary = msg.summary
		}
		return m, nil

	case captureActedMsg:
		if msg.err != nil {
			// Undo optimistic disposition so user can retry.
			delete(m.capturesDisposed, msg.captureID)
			m.errMsg = msg.err.Error()
		}
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
	// Toggle full/short help with '?'. Not active on command screens where '?'
	// could be part of a command string.
	state := m.machine.State()
	if msg.String() == "?" && state != session.StateActive && state != session.StatePaused {
		m.showFullHelp = !m.showFullHelp
		m.helpModel.ShowAll = m.showFullHelp
		return m, nil
	}

	// Arrow key and vim (j/k) cursor navigation for list screens.
	if n := m.listLen(); n > 0 {
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < n-1 {
				m.cursor++
			}
			return m, nil
		case tea.KeyRunes:
			switch msg.String() {
			case "k":
				if m.cursor > 0 {
					m.cursor--
				}
				return m, nil
			case "j":
				if m.cursor < n-1 {
					m.cursor++
				}
				return m, nil
			case "l":
				// 'l' on the project selection screen opens the Reminders list picker
				// for the currently highlighted project (cursor 1..N).
				if state == session.StateSetupProject && m.extra == uiExtraNone &&
					m.remClient != nil &&
					m.cursor >= 1 && m.cursor <= len(m.projects) {
					m.remListsEditIdx = m.cursor - 1
					m.remLists = nil
					m.extra = uiExtraRemListPicker
					m.cursor = 0
					m.input.Reset()
					m.input.Placeholder = "choice"
					return m, m.loadRemListsCmd()
				}
			}
		}
	}

	// End notes uses a textarea; handle it before the textinput path. The
	// reminder-disposition prompt also overlays StateEndingNotes but uses the
	// regular textinput (y/n), so skip the textarea path in that case.
	if m.machine.State() == session.StateEndingNotes && m.extra != uiExtraReminderDispo {
		if msg.Type == tea.KeyEnter {
			return m.handleEndingNotes(strings.TrimSpace(m.noteArea.Value()))
		}
		if msg.Type == tea.KeyTab {
			return m.handleEndingNotesExtend()
		}
		var taCmd tea.Cmd
		m.noteArea, taCmd = m.noteArea.Update(msg)
		return m, taCmd
	}

	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)

	if msg.Type != tea.KeyEnter {
		return m, tiCmd
	}

	val := strings.TrimSpace(m.input.Value())
	// On list screens, an empty Enter selects the cursor item.
	if val == "" {
		if cv := m.cursorVal(); cv != "" {
			val = cv
		}
	}
	m.errMsg = ""

	if m.extra == uiExtraRecover {
		return m.handleRecoverScreen(val)
	}

	if m.extra == uiExtraStart {
		return m.handleStartScreen(val)
	}

	if m.extra == uiExtraReminderDispo {
		return m.handleReminderDisposition(val)
	}

	if m.extra == uiExtraRemListPicker {
		return m.handleRemListPicker(val)
	}

	switch m.machine.State() {
	case session.StateSetupProject:
		return m.handleProjectInput(val)
	case session.StateSetupCategory:
		return m.handleCategoryInput(val)
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

// ── list-screen helpers ───────────────────────────────────────────────────────

// isLoading reports whether an async operation warrants the spinner.
func (m Model) isLoading() bool {
	return (m.remClient != nil && !m.inboxLoaded && m.extra == uiExtraStart) || m.savingSession || m.syncing
}

// syncConfigured reports whether the env-supplied sync config is usable.
func (m Model) syncConfigured() bool {
	return m.syncCfg.URL != "" && m.syncCfg.Token != ""
}

// syncStatusLine returns a short one-line indicator for auto-sync state.
// Returns "" when sync isn't configured, or when no sync has run this session
// and none is in flight. Renders as: "⟳ syncing…", "✓ synced <age>", or
// "✗ sync failed <age>".
func (m Model) syncStatusLine() string {
	if !m.syncConfigured() {
		return ""
	}
	if m.syncing && !m.syncManual {
		return StyleDim.Render("⟳ syncing…")
	}
	if m.lastSyncAt.IsZero() {
		return ""
	}
	age := m.now.Sub(m.lastSyncAt)
	var ageStr string
	switch {
	case age < 10*time.Second:
		ageStr = "just now"
	case age < time.Minute:
		ageStr = fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		ageStr = fmt.Sprintf("%dm ago", int(age.Minutes()))
	default:
		ageStr = fmt.Sprintf("%dh ago", int(age.Hours()))
	}
	if m.lastSyncOK {
		return StyleDim.Render("✓ synced " + ageStr)
	}
	return StyleError.Render("✗ sync failed " + ageStr)
}

// timerPct returns the progress fraction (0.0–1.0) for the active timer or break.
func (m Model) timerPct() float64 {
	switch m.machine.State() {
	case session.StateActive, session.StatePaused:
		planned := time.Duration(m.machine.PlannedMin()) * time.Minute
		if planned == 0 {
			return 0
		}
		remaining := m.machine.TimeRemaining(m.now)
		elapsed := planned - remaining
		if elapsed < 0 {
			elapsed = 0
		}
		pct := float64(elapsed) / float64(planned)
		if pct > 1.0 {
			return 1.0
		}
		return pct
	case session.StateBreak:
		if m.breakEnd.IsZero() {
			return 0
		}
		const breakDur = 5 * time.Minute
		elapsed := m.now.Sub(m.breakEnd.Add(-breakDur))
		if elapsed < 0 {
			return 0
		}
		pct := float64(elapsed) / float64(breakDur)
		if pct > 1.0 {
			return 1.0
		}
		return pct
	}
	return 0
}

// listLen returns the item count for the current list screen (0 = not a list screen).
func (m Model) listLen() int {
	state := m.machine.State()
	switch {
	case m.extra == uiExtraStart:
		n := len(m.inboxItems)
		if m.resume != nil {
			n++
		}
		n += 2 // n) new + q) quit
		if m.syncConfigured() {
			n++ // s) Sync now
		}
		return n
	case m.extra == uiExtraRemListPicker:
		return len(m.remLists) + 1 // 0) none + lists
	case state == session.StateSetupProject && m.extra == uiExtraNone:
		return len(m.projects) + 2 // 0) no-project + projects + n) new
	case state == session.StateSetupCategory && m.extra == uiExtraNone && m.machine.ProjectID() != "":
		return len(m.categories) + len(m.projReminderItems) + 1 // categories + reminders + n) new
	case state == session.StateWhatNext && m.extra == uiExtraNone:
		return 4 // actions 1–4
	}
	return 0
}

// cursorVal maps the current cursor position to the typed-equivalent input
// for the current list screen. Returns "" on non-list screens.
func (m Model) cursorVal() string {
	state := m.machine.State()
	switch {
	case m.extra == uiExtraStart:
		n := len(m.inboxItems)
		if m.cursor < n {
			return strconv.Itoa(m.cursor + 1)
		}
		offset := m.cursor - n
		if m.resume != nil {
			if offset == 0 {
				return "r"
			}
			offset--
		}
		if offset == 0 {
			return "n"
		}
		offset--
		if m.syncConfigured() {
			if offset == 0 {
				return "s"
			}
			offset--
		}
		return "q"

	case state == session.StateSetupProject && m.extra == uiExtraNone:
		if m.cursor == 0 {
			return "0"
		}
		if m.cursor <= len(m.projects) {
			return strconv.Itoa(m.cursor)
		}
		return "n"

	case m.extra == uiExtraRemListPicker:
		if m.cursor == 0 {
			return "0"
		}
		if m.cursor <= len(m.remLists) {
			return strconv.Itoa(m.cursor)
		}
		return "0"

	case state == session.StateSetupCategory && m.extra == uiExtraNone && m.machine.ProjectID() != "":
		if m.cursor < len(m.categories) {
			return strconv.Itoa(m.cursor + 1)
		}
		remOffset := m.cursor - len(m.categories)
		if remOffset < len(m.projReminderItems) {
			return "rem:" + strconv.Itoa(remOffset+1)
		}
		return "n"

	case state == session.StateWhatNext && m.extra == uiExtraNone:
		return strconv.Itoa(m.cursor + 1)
	}
	return ""
}

// ── per-state handlers ────────────────────────────────────────────────────────

// handleStartScreen processes input on the startup screen (start, resume, inbox).
func (m Model) handleStartScreen(val string) (tea.Model, tea.Cmd) {
	val = strings.ToLower(strings.TrimSpace(val))

	// Inbox item selected by number (1..N).
	if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= len(m.inboxItems) {
		item := m.inboxItems[n-1]
		m.doContextText = item.Title
		// Defer Reminders completion until end-of-session disposition prompt,
		// so an aborted session doesn't falsely mark the reminder done.
		m.pendingReminderID = item.ID
		m.pendingReminderTitle = item.Title
		m.inboxItems = nil
		m.extra = uiExtraNone
		m.resume = nil
		m.cursor = 0
		_ = m.machine.BeginSetup()
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, m.loadProjectsCmd()
	}

	switch val {
	case "r":
		if m.resume == nil {
			m.errMsg = "no session to resume"
			m.input.Reset()
			return m, nil
		}
		ri := m.resume
		m.resume = nil
		m.extra = uiExtraNone
		m.selectedCatName = ri.categoryName
		m.selectedProjName = ri.projectName
		_ = m.machine.BeginSetup()
		_ = m.machine.SetProject(ri.projectID, m.now)
		_ = m.machine.SetCategory(ri.categoryID, m.now)
		_ = m.machine.SetDuration(ri.remaining, m.now)
		m.input.Reset()
		m.input.Placeholder = "command"
		return m, m.writeSentinelCmd()

	case "n":
		m.resume = nil
		m.extra = uiExtraNone
		m.cursor = 0
		_ = m.machine.BeginSetup()
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, m.loadProjectsCmd()

	case "s":
		if !m.syncConfigured() {
			m.errMsg = "sync not configured"
			m.input.Reset()
			return m, nil
		}
		if m.syncing {
			m.input.Reset()
			return m, nil
		}
		m.syncing = true
		m.syncManual = true
		m.syncSummary = ""
		m.input.Reset()
		return m, tea.Batch(m.runSyncCmd(true), m.sp.Tick)

	case "q":
		return m, tea.Quit

	default:
		m.errMsg = "invalid choice"
		m.input.Reset()
		return m, nil
	}
}

// handleRecoverScreen handles the y/n crash-recovery prompt.
func (m Model) handleRecoverScreen(val string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "y":
		s := m.crashSentinel
		if s == nil {
			m.errMsg = "no session to recover"
			m.input.Reset()
			return m, nil
		}
		machine, err := session.Hydrate(*s, m.now)
		if err != nil {
			m.errMsg = fmt.Sprintf("recovery failed: %v", err)
			m.crashSentinel = nil
			m.extra = uiExtraNone
			m.initStartup()
			m.input.Reset()
			return m, nil
		}
		m.machine = machine
		m.selectedProjName = s.ProjectName
		m.selectedCatName = s.CategoryName
		m.crashSentinel = nil
		m.extra = uiExtraNone
		m.input.Reset()
		m.input.Placeholder = "command"
		return m, m.writeSentinelCmd()

	case "n":
		_ = recovery.Clear(m.dataDir)
		m.crashSentinel = nil
		m.extra = uiExtraNone
		m.initStartup()
		m.input.Reset()
		var cmds []tea.Cmd
		if m.extra == uiExtraStart && m.remClient != nil {
			cmds = append(cmds, m.loadInboxCmd(), m.sp.Tick)
		} else if m.machine.State() == session.StateSetupProject {
			cmds = append(cmds, m.loadProjectsCmd())
		}
		return m, tea.Batch(cmds...)

	default:
		m.errMsg = "enter y or n"
		m.input.Reset()
		return m, nil
	}
}

func (m Model) handleCategoryInput(val string) (tea.Model, tea.Cmd) {
	// No-project path: free text, dedup by name.
	if m.machine.ProjectID() == "" {
		if val == "" {
			if m.doContextText == "" {
				m.errMsg = "category name required"
				m.input.Reset()
				return m, nil
			}
			val = m.doContextText
		}
		cat, err := m.store.CreateOrGetCategoryByName(m.ctx, val, "")
		if err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.selectedCatName = cat.Name
		m.doContextText = ""
		_ = m.machine.SetCategory(cat.ID, m.now)
		m.input.Reset()
		m.input.Placeholder = "minutes"
		return m, nil
	}

	// Project path: list-based with "n" to create new.
	if m.extra == uiExtraNewName {
		if val == "" {
			if m.doContextText == "" {
				m.errMsg = "name required"
				m.input.Reset()
				return m, nil
			}
			val = m.doContextText
		}
		cat, err := m.store.CreateCategory(m.ctx, val, m.machine.ProjectID())
		if err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.selectedCatName = cat.Name
		m.doContextText = ""
		_ = m.machine.SetCategory(cat.ID, m.now)
		m.extra = uiExtraNone
		m.input.Reset()
		m.input.Placeholder = "minutes"
		return m, nil
	}

	if strings.ToLower(val) == "n" {
		m.extra = uiExtraNewName
		m.input.Reset()
		m.input.Placeholder = "category name"
		return m, nil
	}

	// Reminder selected from the project-reminders section.
	if strings.HasPrefix(val, "rem:") {
		n, err := strconv.Atoi(strings.TrimPrefix(val, "rem:"))
		if err != nil || n < 1 || n > len(m.projReminderItems) {
			m.errMsg = "invalid choice"
			m.input.Reset()
			return m, nil
		}
		rem := m.projReminderItems[n-1]
		m.doContextText = rem.Title
		m.pendingReminderID = rem.ID
		m.pendingReminderTitle = rem.Title
		m.projReminderItems = nil // clear section; context shown via doContextText
		m.cursor = 0
		m.input.Reset()
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
	m.input.Placeholder = "minutes"
	return m, nil
}

func (m Model) handleRemListPicker(val string) (tea.Model, tea.Cmd) {
	proj := m.projects[m.remListsEditIdx]
	var listName string
	if val == "0" {
		// clear — listName stays ""
	} else if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= len(m.remLists) {
		listName = m.remLists[n-1]
	} else {
		m.errMsg = "enter 0 to clear, or 1-" + strconv.Itoa(len(m.remLists)) + " to choose"
		m.input.Reset()
		return m, nil
	}
	if err := m.store.SetProjectRemindersList(m.ctx, proj.ID, listName); err != nil {
		m.errMsg = err.Error()
		m.input.Reset()
		return m, nil
	}
	m.projects[m.remListsEditIdx].RemindersListName = listName
	m.extra = uiExtraNone
	m.remLists = nil
	m.cursor = m.remListsEditIdx + 1 // return cursor to the edited project
	m.input.Reset()
	m.input.Placeholder = "choice"
	return m, nil
}

func (m Model) handleProjectInput(val string) (tea.Model, tea.Cmd) {
	if m.extra == uiExtraNewName {
		if val == "" {
			m.errMsg = "name required"
			m.input.Reset()
			return m, nil
		}
		proj, err := m.store.CreateProject(m.ctx, val)
		if err != nil {
			m.errMsg = err.Error()
			m.input.Reset()
			return m, nil
		}
		m.selectedProjName = proj.Name
		m.selectedProjRemindersList = ""
		m.projReminderItems = nil
		_ = m.machine.SetProject(proj.ID, m.now)
		m.extra = uiExtraNone
		m.cursor = 0
		m.input.Reset()
		m.input.Placeholder = "choice"
		return m, m.loadCategoriesCmd()
	}

	if strings.ToLower(val) == "n" {
		m.extra = uiExtraNewName
		m.input.Reset()
		m.input.Placeholder = "project name"
		return m, nil
	}

	// "0" means no project — go to category as free-text input.
	if val == "0" {
		m.selectedProjName = ""
		m.selectedProjRemindersList = ""
		m.projReminderItems = nil
		_ = m.machine.SetProject("", m.now)
		m.input.Reset()
		m.input.Placeholder = "category name"
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
	m.selectedProjRemindersList = proj.RemindersListName
	m.projReminderItems = nil
	_ = m.machine.SetProject(proj.ID, m.now)
	m.cursor = 0
	m.input.Reset()
	m.input.Placeholder = "choice"
	var cmds []tea.Cmd
	cmds = append(cmds, m.loadCategoriesCmd())
	if proj.RemindersListName != "" && m.remClient != nil {
		cmds = append(cmds, m.loadProjRemindersCmd(proj.RemindersListName))
	}
	return m, tea.Batch(cmds...)
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
	return m, m.writeSentinelCmd()
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

	var needSentinel bool
	switch cmd {
	case "upn":
		if arg == "" {
			m.errMsg = "usage: upn <text>"
		} else if err := m.machine.AddCapture(arg, now); err != nil {
			m.errMsg = err.Error()
		} else {
			needSentinel = true
		}
	case "ext":
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			m.errMsg = "usage: ext <minutes>"
		} else if err := m.machine.Extend(n, now); err != nil {
			m.errMsg = err.Error()
		} else {
			needSentinel = true
		}
	case "pause":
		if m.machine.State() == session.StatePaused {
			m.errMsg = "already paused — use resume"
		} else if err := m.machine.Pause(now); err != nil {
			m.errMsg = err.Error()
		} else {
			needSentinel = true
		}
	case "resume":
		if m.machine.State() == session.StateActive {
			m.errMsg = "not paused"
		} else if err := m.machine.Resume(now); err != nil {
			m.errMsg = err.Error()
		} else {
			needSentinel = true
		}
	case "end":
		if err := m.machine.End(now); err != nil {
			m.errMsg = err.Error()
		} else {
			m.enterEndingNotes()
		}
	case "cancel":
		_ = m.machine.Cancel(now)
		m.initStartup()
		m.input.Reset()
		var cmds []tea.Cmd
		cmds = append(cmds, m.clearSentinelCmd())
		if m.extra == uiExtraStart && m.remClient != nil {
			cmds = append(cmds, m.loadInboxCmd(), m.sp.Tick)
		} else if m.machine.State() == session.StateSetupProject {
			cmds = append(cmds, m.loadProjectsCmd())
		}
		return m, tea.Batch(cmds...)
	default:
		m.errMsg = fmt.Sprintf("unknown command %q", cmd)
	}

	m.input.Reset()
	if needSentinel {
		return m, m.writeSentinelCmd()
	}
	return m, nil
}

// enterEndingNotes resets the note textarea and focuses it. Call at every
// transition into StateEndingNotes.
func (m *Model) enterEndingNotes() {
	m.noteArea.SetValue("")
	if m.width > 0 {
		m.noteArea.SetWidth(max(m.width-4, 20))
	}
	m.noteArea.Focus()
}

func (m Model) handleEndingNotes(val string) (tea.Model, tea.Cmd) {
	// If this session was started from an inbox reminder, ask before
	// marking it complete in Reminders. Save is deferred until the user
	// answers y/n.
	if m.pendingReminderID != "" {
		m.pendingEndNotes = val
		m.extra = uiExtraReminderDispo
		m.input.Reset()
		m.input.Placeholder = "y / n"
		m.input.Focus()
		return m, nil
	}
	_ = m.machine.SetEndNotes(val, m.now)
	m.savingSession = true
	return m, tea.Batch(m.saveSessionCmd(), m.sp.Tick)
}

// tabExtendMinutes is the default extension applied when the user hits Tab
// on the end-notes screen to undo an accidental end. The user can extend
// further from Active with "ext <n>".
const tabExtendMinutes = 5

// handleEndingNotesExtend is the Tab shortcut: undo End, extend the session
// by tabExtendMinutes, and go back to Active. Any text in the note textarea
// is discarded — the session is continuing, so there are no "end notes" yet.
func (m Model) handleEndingNotesExtend() (tea.Model, tea.Cmd) {
	if err := m.machine.ResumeFromEndingNotes(m.now); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	if err := m.machine.Extend(tabExtendMinutes, m.now); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.noteArea.SetValue("")
	m.noteArea.Blur()
	m.input.Reset()
	m.input.Placeholder = "command"
	m.input.Focus()
	return m, m.writeSentinelCmd()
}

// handleReminderDisposition resolves the post-session prompt asking whether to
// mark the originating Reminders item complete. y → mark complete + save;
// n → leave reminder active + save.
func (m Model) handleReminderDisposition(val string) (tea.Model, tea.Cmd) {
	v := strings.ToLower(strings.TrimSpace(val))
	if v != "y" && v != "n" {
		m.errMsg = "press y or n"
		m.input.Reset()
		return m, nil
	}
	reminderID := m.pendingReminderID
	notes := m.pendingEndNotes
	m.pendingReminderID = ""
	m.pendingReminderTitle = ""
	m.pendingEndNotes = ""
	m.extra = uiExtraNone
	m.errMsg = ""
	_ = m.machine.SetEndNotes(notes, m.now)
	m.savingSession = true
	cmds := []tea.Cmd{m.saveSessionCmd(), m.sp.Tick}
	if v == "y" && reminderID != "" && m.remClient != nil {
		cmds = append(cmds, m.completeInboxItemCmd(reminderID))
	}
	return m, tea.Batch(cmds...)
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
		m.doContextText = ""
		m.capturesDisposed = nil
		m.input.Reset()
		m.input.Placeholder = "command"
		return m, m.writeSentinelCmd()
	}

	// Capture disposition commands: c<n>, r<n>, d<n>.
	if len(val) >= 2 {
		action := val[0]
		idx, err := strconv.Atoi(val[1:])
		if err == nil && (action == 'c' || action == 'r' || action == 'd') {
			caps := m.undisposedCaptures()
			if idx < 1 || idx > len(caps) {
				m.errMsg = "invalid capture number"
				m.input.Reset()
				return m, nil
			}
			cap := caps[idx-1]
			if m.capturesDisposed == nil {
				m.capturesDisposed = make(map[string]bool)
			}
			m.capturesDisposed[cap.ID] = true
			m.input.Reset()
			switch action {
			case 'c':
				return m, m.clearCaptureCmd(cap.ID)
			case 'r':
				if m.remClient == nil {
					m.errMsg = "Reminders not available"
					delete(m.capturesDisposed, cap.ID)
					return m, nil
				}
				return m, m.sendToRemindersCmd(cap.ID, cap.Text)
			case 'd':
				m.doContextText = cap.Text
				allCaps := m.machine.Captures()
				_ = m.machine.NewSession(m.now)
				m.selectedCatName = ""
				m.selectedProjName = ""
				m.capturesDisposed = nil
				m.cursor = 0
				m.doContextText = allCaps[idx-1].Text
				return m, tea.Batch(m.clearCaptureCmd(cap.ID), m.loadProjectsCmd())
			}
		}
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
		m.doContextText = ""
		m.capturesDisposed = nil
		m.cursor = 0
		m.selectedCatName = ""
		m.selectedProjName = ""
		m.input.Reset()
		return m, m.loadProjectsCmd()
	case "3":
		_ = m.machine.StartBreak(now)
		m.breakEnd = now.Add(5 * time.Minute)
		m.input.Reset()
		m.input.Placeholder = "enter to end early"
	case "4":
		_ = m.machine.Finish(now)
		return m, tea.Quit
	default:
		m.errMsg = "enter 1-4 or c/r/d + capture number"
		m.input.Reset()
	}
	return m, nil
}

// undisposedCaptures returns the subset of machine captures not yet acted on.
func (m Model) undisposedCaptures() []session.Capture {
	caps := m.machine.Captures()
	if len(m.capturesDisposed) == 0 {
		return caps
	}
	out := caps[:0:0]
	for _, c := range caps {
		if !m.capturesDisposed[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// ── async commands ────────────────────────────────────────────────────────────

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadCategoriesCmd() tea.Cmd {
	ctx, s, projID := m.ctx, m.store, m.machine.ProjectID()
	return func() tea.Msg {
		cats, err := s.ListCategoriesByProject(ctx, projID)
		return categoriesLoadedMsg{cats: cats, err: err}
	}
}

func (m Model) loadProjectsCmd() tea.Cmd {
	ctx, s := m.ctx, m.store
	return func() tea.Msg {
		projs, err := s.ListProjects(ctx)
		return projectsLoadedMsg{projs: projs, err: err}
	}
}

func (m Model) runSyncCmd(manual bool) tea.Cmd {
	ctx, s, cfg := m.ctx, m.store, m.syncCfg
	return func() tea.Msg {
		var buf bytes.Buffer
		err := syncclient.Sync(ctx, s, cfg, &buf)
		return syncDoneMsg{summary: strings.TrimSpace(buf.String()), err: err, manual: manual}
	}
}

func (m Model) loadInboxCmd() tea.Cmd {
	rc := m.remClient
	ctx := m.ctx
	return func() tea.Msg {
		items, err := rc.ListInbox(ctx)
		return inboxLoadedMsg{items: items, err: err}
	}
}

func (m Model) loadRemListsCmd() tea.Cmd {
	rc := m.remClient
	ctx := m.ctx
	return func() tea.Msg {
		lists, err := rc.Lists(ctx)
		return remListsLoadedMsg{lists: lists, err: err}
	}
}

func (m Model) loadProjRemindersCmd(listName string) tea.Cmd {
	rc := m.remClient
	ctx := m.ctx
	return func() tea.Msg {
		items, err := rc.ListItems(ctx, listName)
		return projRemindersLoadedMsg{items: items, err: err}
	}
}

func (m Model) clearCaptureCmd(captureID string) tea.Cmd {
	s, ctx := m.store, m.ctx
	return func() tea.Msg {
		return captureActedMsg{captureID: captureID, err: s.MarkCaptureCleared(ctx, captureID)}
	}
}

func (m Model) sendToRemindersCmd(captureID, text string) tea.Cmd {
	s, rc, ctx := m.store, m.remClient, m.ctx
	return func() tea.Msg {
		if _, err := rc.Add(ctx, text); err != nil {
			return captureActedMsg{captureID: captureID, err: err}
		}
		return captureActedMsg{captureID: captureID, err: s.MarkCaptureSentToReminders(ctx, captureID)}
	}
}

func (m Model) completeInboxItemCmd(reminderID string) tea.Cmd {
	rc, ctx := m.remClient, m.ctx
	return func() tea.Msg {
		// Failures are silent — the item stays in Reminders.
		_ = rc.Complete(ctx, reminderID)
		return nil
	}
}

// writeSentinelCmd snapshots the current machine state and writes active.json.
// Best-effort: errors are logged to stderr but not surfaced to the user.
func (m Model) writeSentinelCmd() tea.Cmd {
	if m.dataDir == "" {
		return nil
	}
	snap, err := m.machine.Snapshot(m.now, m.selectedProjName, m.selectedCatName)
	if err != nil {
		return nil // machine not in Active/Paused — skip
	}
	dataDir := m.dataDir
	return func() tea.Msg {
		if werr := recovery.Write(dataDir, snap); werr != nil {
			fmt.Fprintf(os.Stderr, "sentinel write: %v\n", werr)
		}
		return nil
	}
}

// clearSentinelCmd removes active.json after a clean session save.
func (m Model) clearSentinelCmd() tea.Cmd {
	if m.dataDir == "" {
		return nil
	}
	dataDir := m.dataDir
	return func() tea.Msg {
		if err := recovery.Clear(dataDir); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel clear: %v\n", err)
		}
		return nil
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
			if _, err := s.CreateCapture(ctx, c.ID, sess.ID, c.Text); err != nil {
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
	// StateIdle is terminal only after a session has ended via "4) End".
	// During startup the machine is also Idle but extra == uiExtraStart or
	// uiExtraRecover, so the early return must only fire when extra is None.
	if m.machine.State() == session.StateIdle && m.extra == uiExtraNone {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")
	m.renderContent(&b)
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

// renderHeader produces the 3-column header and separator line.
func (m Model) renderHeader() string {
	left := StyleTitle.Render("jacktasks")

	name := m.screenName()
	middle := StyleHeader.Render(name)

	right := ""
	state := m.machine.State()
	if state == session.StateActive || state == session.StatePaused {
		rem := m.machine.TimeRemaining(m.now)
		planned := time.Duration(m.machine.PlannedMin()) * time.Minute
		right = StyleDim.Render(fmt.Sprintf("%s/%s  %s/%s",
			projectLabel(m.selectedProjName), m.selectedCatName,
			formatDuration(rem), formatDuration(planned)))
	}

	w := m.width
	if w == 0 {
		w = 80
	}

	leftW := lipgloss.Width(left)
	middleW := lipgloss.Width(middle)
	rightW := lipgloss.Width(right)

	available := w - leftW - middleW - rightW
	var line string
	if available < 2 {
		line = left + "  " + middle
		if right != "" {
			line += "  " + right
		}
	} else {
		leftPad := available / 2
		rightPad := available - leftPad
		line = left + strings.Repeat(" ", leftPad) + middle + strings.Repeat(" ", rightPad) + right
	}

	w = max(w, lipgloss.Width(line))
	sep := StyleBorder.Render(strings.Repeat("─", w))
	return line + "\n" + sep
}

// renderFooter produces the separator and key-hint line.
func (m Model) renderFooter() string {
	w := m.width
	if w == 0 {
		w = 80
	}
	sep := StyleBorder.Render(strings.Repeat("─", w))
	hint := StyleFooter.Render(m.footerHint())
	return sep + "\n" + hint
}

// footerHint returns a short key-hint string for the current screen.
// For command screens (Active/Paused) we render plain text because those
// commands are free-typed and bubbles/help skips bindings with no key triggers.
func (m Model) footerHint() string {
	switch m.machine.State() {
	case session.StateActive:
		return "upn <text> capture  •  ext <n> extend  •  pause  •  end  •  cancel"
	case session.StatePaused:
		return "upn <text> capture  •  ext <n> extend  •  resume  •  end  •  cancel"
	}
	// Project selection: show 'l' hint when reminders are available.
	if m.machine.State() == session.StateSetupProject && m.extra == uiExtraNone && m.remClient != nil {
		return "↑↓ / j/k navigate  •  l set reminders list  •  enter select  •  q quit"
	}
	if m.width > 0 {
		m.helpModel.Width = m.width
	}
	return m.helpModel.View(m.currentKeyMap())
}

// screenName returns the display label for the current screen (used in header).
func (m Model) screenName() string {
	if m.extra == uiExtraRecover {
		return "Recover Session"
	}
	if m.extra == uiExtraStart {
		return "Start"
	}
	if m.extra == uiExtraContinueDur {
		return "Duration"
	}
	if m.extra == uiExtraRemListPicker {
		return "Set Reminders List"
	}
	switch m.machine.State() {
	case session.StateSetupProject:
		if m.extra == uiExtraNewName {
			return "New Project"
		}
		return "Select Project"
	case session.StateSetupCategory:
		if m.extra == uiExtraNewName {
			return "New Category"
		}
		if m.machine.ProjectID() == "" {
			return "Enter Category"
		}
		return "Select Category"
	case session.StateSetupDuration:
		return "Duration"
	case session.StateActive:
		return "Active"
	case session.StatePaused:
		return "Paused"
	case session.StateEndingNotes:
		if m.extra == uiExtraReminderDispo {
			return "Reminder Done?"
		}
		return "End Notes"
	case session.StateWhatNext:
		return "What Next"
	case session.StateBreak:
		return "Break"
	}
	return ""
}

// currentKeyMap returns the appropriate key map for the footer hint line.
func (m Model) currentKeyMap() help.KeyMap {
	switch m.machine.State() {
	case session.StateActive:
		return kmActive
	case session.StatePaused:
		return kmPaused
	}
	if m.listLen() > 0 {
		return kmList
	}
	return kmText
}

// renderContent writes the body content for the current screen.
func (m Model) renderContent(b *strings.Builder) {
	machState := m.machine.State()

	// Crash-recovery offer.
	if m.extra == uiExtraRecover {
		if s := m.crashSentinel; s != nil {
			startedAt := time.Unix(s.StartedAt, 0)
			agoMin := int(m.now.Sub(startedAt).Minutes())
			proj := s.ProjectName
			if proj == "" {
				proj = "—"
			}
			fmt.Fprintf(b, "  Recover unfinished session?\n\n")
			fmt.Fprintf(b, "  %s / %s — started %dm ago, %d capture(s)\n",
				StyleAccent.Render(proj), StyleAccent.Render(s.CategoryName),
				agoMin, len(s.Captures))
			fmt.Fprintf(b, "  Planned: %dm\n\n", s.PlannedDurationMin)
			fmt.Fprintf(b, "  %s  %s\n\n",
				StyleSelected.Render("y) Resume"),
				StyleDim.Render("n) Discard"))
		}
		b.WriteString("  ")
		b.WriteString(m.input.View())
		writeErr(b, m.errMsg)
		return
	}

	// Startup screen.
	if m.extra == uiExtraStart {
		m.renderStartScreen(b)
		return
	}

	switch machState {
	case session.StateSetupProject:
		if m.doContextText != "" {
			fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("Doing: "+m.doContextText))
		}
		if m.extra == uiExtraRemListPicker {
			proj := m.projects[m.remListsEditIdx]
			fmt.Fprintf(b, "  Set Reminders list for %s:\n\n", StyleAccent.Render(proj.Name))
			if m.remLists == nil {
				fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("Loading lists…"))
			} else {
				items := []string{"0) None (clear)"}
				for i, l := range m.remLists {
					label := fmt.Sprintf("%d) %s", i+1, l)
					if l == proj.RemindersListName {
						label += StyleDim.Render("  ← current")
					}
					items = append(items, label)
				}
				for i, item := range items {
					fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == i, item))
				}
				fmt.Fprintln(b)
			}
		} else if m.extra == uiExtraNewName {
			fmt.Fprintf(b, "  New project name:\n\n")
		} else {
			fmt.Fprintf(b, "  Select a project:\n\n")
			items := []string{"0) — no project"}
			for i, p := range m.projects {
				label := fmt.Sprintf("%d) %s", i+1, p.Name)
				if p.RemindersListName != "" {
					label += StyleDim.Render("  ["+p.RemindersListName+"]")
				}
				items = append(items, label)
			}
			items = append(items, "n) New project")
			for i, item := range items {
				fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == i, item))
			}
			fmt.Fprintln(b)
		}

	case session.StateSetupCategory:
		if m.doContextText != "" {
			fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("Doing: "+m.doContextText))
		}
		if m.machine.ProjectID() == "" {
			fmt.Fprintf(b, "  Enter a category:\n\n")
			if m.doContextText != "" {
				fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("(press Enter to use the text above, or type a new name)"))
			}
		} else {
			fmt.Fprintf(b, "  Select a category for %s:\n\n", StyleAccent.Render(m.selectedProjName))
			if m.extra == uiExtraNewName {
				fmt.Fprintf(b, "  New category name:\n\n")
				if m.doContextText != "" {
					fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("(press Enter to use the text above, or type a new name)"))
				}
			} else {
				// Existing project categories.
				for i, c := range m.categories {
					label := fmt.Sprintf("%d) %s", i+1, c.Name)
					fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == i, label))
				}
				// Per-project Reminders section (when the project has an associated list).
				if len(m.projReminderItems) > 0 {
					fmt.Fprintf(b, "\n  %s\n", StyleDim.Render("From "+m.selectedProjRemindersList+":"))
					for i, r := range m.projReminderItems {
						idx := len(m.categories) + i
						fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == idx, "• "+r.Title))
					}
					fmt.Fprintln(b)
				}
				// New category.
				newIdx := len(m.categories) + len(m.projReminderItems)
				fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == newIdx, "n) New category"))
				fmt.Fprintln(b)
			}
		}

	case session.StateSetupDuration:
		fmt.Fprintf(b, "  Planned duration (minutes):\n\n")

	case session.StateActive:
		rem := m.machine.TimeRemaining(m.now)
		planned := time.Duration(m.machine.PlannedMin()) * time.Minute
		fmt.Fprintf(b, "  %s\n\n", StyleActive.Render("■ Active"))
		prog := m.prog
		if m.width > 0 {
			prog.Width = m.width - 8
		}
		fmt.Fprintf(b, "  %s\n", prog.View())
		fmt.Fprintf(b, "  %s  %s\n\n",
			StyleTimer.Render(formatDuration(rem)),
			StyleDim.Render("/ "+formatDuration(planned)))

	case session.StatePaused:
		rem := m.machine.TimeRemaining(m.now)
		planned := time.Duration(m.machine.PlannedMin()) * time.Minute
		fmt.Fprintf(b, "  %s\n\n", StylePaused.Render("⏸  Paused"))
		prog := m.prog
		if m.width > 0 {
			prog.Width = m.width - 8
		}
		fmt.Fprintf(b, "  %s\n", prog.View())
		fmt.Fprintf(b, "  %s  %s\n\n",
			StyleTimer.Render(formatDuration(rem)),
			StyleDim.Render("/ "+formatDuration(planned)))

	case session.StateEndingNotes:
		if m.extra == uiExtraReminderDispo {
			fmt.Fprintf(b, "  Mark reminder complete?\n\n")
			fmt.Fprintf(b, "  %s\n\n", StyleAccent.Render("\""+m.pendingReminderTitle+"\""))
			fmt.Fprintf(b, "  %s  %s\n\n",
				StyleSelected.Render("y) Yes, mark complete"),
				StyleDim.Render("n) No, keep it active"))
			break
		}
		writeCaptureList(b, m.machine.Captures())
		if m.noteArea.Value() == "" {
			banner := fmt.Sprintf("  ▶  SESSION ENDED — Enter to skip · Tab for +%dm  ◀", tabExtendMinutes)
			style := StyleFlashOff
			if m.flashOn {
				style = StyleFlashOn
			}
			fmt.Fprintf(b, "%s\n\n", style.Render(banner))
		} else {
			fmt.Fprintf(b, "  End notes (Enter to save · Tab for +%dm):\n\n", tabExtendMinutes)
		}

	case session.StateWhatNext:
		m.renderWhatNext(b)
		return // renderWhatNext writes the input line itself

	case session.StateBreak:
		rem := m.breakEnd.Sub(m.now)
		if rem < 0 {
			rem = 0
		}
		fmt.Fprintf(b, "  %s\n\n", StyleTimer.Render("☕ Break"))
		prog := m.prog
		if m.width > 0 {
			prog.Width = m.width - 8
		}
		fmt.Fprintf(b, "  %s\n", prog.View())
		fmt.Fprintf(b, "  %s\n\n", StyleDim.Render(formatDuration(rem)+" remaining"))
		fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("Press Enter to end early"))
	}

	if m.machine.State() == session.StateEndingNotes && m.extra != uiExtraReminderDispo {
		b.WriteString(m.noteArea.View())
	} else {
		b.WriteString("  ")
		b.WriteString(m.input.View())
	}
	writeErr(b, m.errMsg)
}

// renderStartScreen renders the startup screen (inbox + resume + new/quit).
func (m Model) renderStartScreen(b *strings.Builder) {
	if logo := renderLogo(m.width); logo != "" {
		b.WriteString(logo)
		fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("v"+Version))
	} else {
		b.WriteByte('\n')
	}

	if m.remClient != nil && !m.inboxLoaded {
		fmt.Fprintf(b, "  %s Checking inbox...\n\n", m.sp.View())
		b.WriteString("  ")
		b.WriteString(m.input.View())
		writeErr(b, m.errMsg)
		return
	}

	if m.syncing && m.syncManual {
		fmt.Fprintf(b, "  %s Syncing...\n\n", m.sp.View())
		b.WriteString("  ")
		b.WriteString(m.input.View())
		writeErr(b, m.errMsg)
		return
	}

	cursorIdx := 0

	if len(m.inboxItems) > 0 {
		fmt.Fprintf(b, "  %s\n\n", StyleHeader.Render("Inbox"))
		for _, item := range m.inboxItems {
			fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == cursorIdx,
				fmt.Sprintf("%d) %s", cursorIdx+1, item.Title)))
			cursorIdx++
		}
		fmt.Fprintln(b)
	}

	if m.resume != nil {
		ri := m.resume
		fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == cursorIdx,
			fmt.Sprintf("r) Resume %s / %s (%d min remaining)",
				projectLabel(ri.projectName), ri.categoryName, ri.remaining)))
		cursorIdx++
	}
	fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == cursorIdx, "n) New session"))
	cursorIdx++
	if m.syncConfigured() {
		fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == cursorIdx, "s) Sync now"))
		cursorIdx++
	}
	fmt.Fprintf(b, "  %s\n\n", renderListItem(m.cursor == cursorIdx, "q) Quit"))

	if status := m.syncStatusLine(); status != "" {
		fmt.Fprintf(b, "  %s\n", status)
	}
	if m.syncSummary != "" {
		for _, line := range strings.Split(m.syncSummary, "\n") {
			fmt.Fprintf(b, "  %s\n", StyleDim.Render(line))
		}
	}
	if m.syncSummary != "" || m.syncStatusLine() != "" {
		fmt.Fprintln(b)
	}

	b.WriteString("  ")
	b.WriteString(m.input.View())
	writeErr(b, m.errMsg)
}

// renderWhatNext renders the WhatNext screen including captures and actions.
func (m Model) renderWhatNext(b *strings.Builder) {
	if m.extra == uiExtraContinueDur {
		fmt.Fprintf(b, "  Duration for next session (minutes):\n\n")
		b.WriteString("  ")
		b.WriteString(m.input.View())
		writeErr(b, m.errMsg)
		return
	}

	if m.savingSession {
		fmt.Fprintf(b, "  %s Saving session...\n\n", m.sp.View())
	}

	pending := m.undisposedCaptures()
	if len(pending) > 0 {
		fmt.Fprintf(b, "  %s\n\n", StyleHeader.Render("Captures"))
		for i, c := range pending {
			fmt.Fprintf(b, "  %d) %s\n", i+1, c.Text)
		}
		fmt.Fprintln(b)
		if m.remClient != nil {
			fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("c<n> clear  r<n> → Reminders  d<n> do now"))
		} else {
			fmt.Fprintf(b, "  %s\n\n", StyleDim.Render("c<n> clear  d<n> do now"))
		}
	}

	fmt.Fprintf(b, "  %s\n\n", StyleHeader.Render("What next?"))
	actions := []string{
		"1) Continue session",
		"2) New session",
		"3) Break (5 min)",
		"4) End",
	}
	for i, a := range actions {
		fmt.Fprintf(b, "  %s\n", renderListItem(m.cursor == i, a))
	}
	fmt.Fprintln(b)

	if status := m.syncStatusLine(); status != "" {
		fmt.Fprintf(b, "  %s\n\n", status)
	}

	b.WriteString("  ")
	b.WriteString(m.input.View())
	writeErr(b, m.errMsg)
}

// ── view helpers ──────────────────────────────────────────────────────────────

// renderListItem renders a single selectable item with a cursor indicator.
func renderListItem(selected bool, label string) string {
	if selected {
		return StyleCursor.Render("▶") + " " + StyleSelected.Render(label)
	}
	return "  " + label
}

func writeCaptureList(b *strings.Builder, caps []session.Capture) {
	if len(caps) == 0 {
		return
	}
	fmt.Fprintf(b, "  %s\n\n", StyleHeader.Render("Captures"))
	for _, c := range caps {
		fmt.Fprintf(b, "  • %s\n", c.Text)
	}
	fmt.Fprintln(b)
}

func writeErr(b *strings.Builder, msg string) {
	if msg != "" {
		fmt.Fprintf(b, "\n  %s", StyleError.Render(msg))
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

func projectLabel(name string) string {
	if name == "" {
		return "—"
	}
	return name
}
