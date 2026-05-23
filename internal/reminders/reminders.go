// Package reminders wraps Apple Reminders (via go-eventkit) behind a thin
// interface so the rest of the codebase can be tested without cgo or TCC
// prompts.
//
// The real EventKit client is created by NewEventKit. Use NewFake for tests.
package reminders

import "context"

const InboxListName = "jacktasks-inbox"

// Reminder is a trimmed view of a Reminders item — just what jacktasks needs.
type Reminder struct {
	ID    string
	Title string
}

// Client is the interface the TUI uses to interact with Apple Reminders.
// Implementations: eventkitClient (real) and Fake (tests).
type Client interface {
	// ListInbox returns incomplete reminders from jacktasks-inbox.
	ListInbox(ctx context.Context) ([]Reminder, error)
	// Add creates a new reminder in jacktasks-inbox and returns its ID.
	Add(ctx context.Context, text string) (string, error)
	// Complete marks the reminder with the given ID as done.
	Complete(ctx context.Context, id string) error
}
