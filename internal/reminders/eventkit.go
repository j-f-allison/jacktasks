package reminders

import (
	"context"
	"fmt"

	ekr "github.com/BRO3886/go-eventkit/reminders"
)

type eventkitClient struct {
	c *ekr.Client
}

// NewEventKit creates a real Reminders client backed by Apple EventKit.
// On the first call macOS may show a TCC permission prompt. Returns an error if
// access is denied or the platform is not darwin.
func NewEventKit() (Client, error) {
	c, err := ekr.New()
	if err != nil {
		return nil, fmt.Errorf("reminders: %w", err)
	}
	return &eventkitClient{c: c}, nil
}

func (e *eventkitClient) Lists(_ context.Context) ([]string, error) {
	lists, err := e.c.Lists()
	if err != nil {
		return nil, fmt.Errorf("list reminders lists: %w", err)
	}
	out := make([]string, len(lists))
	for i, l := range lists {
		out[i] = l.Title
	}
	return out, nil
}

func (e *eventkitClient) ListInbox(ctx context.Context) ([]Reminder, error) {
	return e.ListItems(ctx, InboxListName)
}

func (e *eventkitClient) ListItems(_ context.Context, listName string) ([]Reminder, error) {
	completed := false
	items, err := e.c.Reminders(
		ekr.WithList(listName),
		ekr.WithCompleted(completed),
	)
	if err != nil {
		return nil, fmt.Errorf("list items from %q: %w", listName, err)
	}
	out := make([]Reminder, len(items))
	for i, r := range items {
		out[i] = Reminder{ID: r.ID, Title: r.Title}
	}
	return out, nil
}

func (e *eventkitClient) Add(_ context.Context, text string) (string, error) {
	r, err := e.c.CreateReminder(ekr.CreateReminderInput{
		Title:    text,
		ListName: InboxListName,
	})
	if err != nil {
		return "", fmt.Errorf("create reminder: %w", err)
	}
	return r.ID, nil
}

func (e *eventkitClient) Complete(_ context.Context, id string) error {
	if _, err := e.c.CompleteReminder(id); err != nil {
		return fmt.Errorf("complete reminder %q: %w", id, err)
	}
	return nil
}
