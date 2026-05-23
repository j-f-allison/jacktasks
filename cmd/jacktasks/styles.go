package main

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// ── color palette ─────────────────────────────────────────────────────────────

var (
	colorPrimary = lipgloss.AdaptiveColor{Light: "#7B2FBE", Dark: "#C97BFA"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#0A7C68", Dark: "#4EC8B4"}
	colorError   = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#FF5555"}
	colorBorder  = lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#444444"}
)

// ── named styles ──────────────────────────────────────────────────────────────

var (
	StyleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	StyleAccent   = lipgloss.NewStyle().Foreground(colorSuccess)
	StyleDim      = lipgloss.NewStyle().Faint(true)
	StyleError    = lipgloss.NewStyle().Foreground(colorError)
	StyleSelected = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	StyleCursor   = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	StyleHeader   = lipgloss.NewStyle().Bold(true)
	StyleFooter   = lipgloss.NewStyle().Faint(true)
	StyleTimer    = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	StyleActive   = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	StylePaused   = lipgloss.NewStyle().Bold(true).Faint(true)
	StyleBorder   = lipgloss.NewStyle().Foreground(colorBorder)
)

// ── key bindings ──────────────────────────────────────────────────────────────

var (
	kbUp    = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up"))
	kbDown  = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down"))
	kbEnter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	kbQuit  = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^C", "quit"))
	kbHelp  = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more keys"))

	// Hint-only bindings: no key triggers, shown in footer as command reference.
	kbUpn    = key.NewBinding(key.WithHelp("upn <text>", "capture thought"))
	kbExt    = key.NewBinding(key.WithHelp("ext <n>", "extend timer"))
	kbPause  = key.NewBinding(key.WithHelp("pause", "pause"))
	kbResume = key.NewBinding(key.WithHelp("resume", "resume"))
	kbEnd    = key.NewBinding(key.WithHelp("end", "end session"))
)

// ── key maps ──────────────────────────────────────────────────────────────────

// screenKeyMap implements help.KeyMap for a given screen.
type screenKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k screenKeyMap) ShortHelp() []key.Binding  { return k.short }
func (k screenKeyMap) FullHelp() [][]key.Binding { return k.full }

// Compile-time interface check.
var _ help.KeyMap = screenKeyMap{}

var (
	kmList = screenKeyMap{
		short: []key.Binding{kbUp, kbDown, kbEnter, kbQuit},
		full:  [][]key.Binding{{kbUp, kbDown, kbEnter, kbHelp, kbQuit}},
	}
	kmActive = screenKeyMap{
		short: []key.Binding{kbUpn, kbPause, kbEnd},
		full:  [][]key.Binding{{kbUpn, kbExt, kbPause, kbEnd}},
	}
	kmPaused = screenKeyMap{
		short: []key.Binding{kbUpn, kbResume, kbEnd},
		full:  [][]key.Binding{{kbUpn, kbExt, kbResume, kbEnd}},
	}
	kmText = screenKeyMap{
		short: []key.Binding{kbEnter, kbQuit},
		full:  [][]key.Binding{{kbEnter, kbQuit}},
	}
)
