package reminders

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Fake is an in-memory Client for use in tests.
// Pre-load Inbox to simulate existing reminders.
type Fake struct {
	Inbox     []Reminder // incomplete reminders visible to ListInbox
	AddErr    error      // if non-nil, Add returns this error
	CompleteErr error    // if non-nil, Complete returns this error
}

func (f *Fake) ListInbox(_ context.Context) ([]Reminder, error) {
	out := make([]Reminder, len(f.Inbox))
	copy(out, f.Inbox)
	return out, nil
}

func (f *Fake) Add(_ context.Context, text string) (string, error) {
	if f.AddErr != nil {
		return "", f.AddErr
	}
	id := uuid.NewString()
	f.Inbox = append(f.Inbox, Reminder{ID: id, Title: text})
	return id, nil
}

func (f *Fake) Complete(_ context.Context, id string) error {
	if f.CompleteErr != nil {
		return f.CompleteErr
	}
	for i, r := range f.Inbox {
		if r.ID == id {
			f.Inbox = append(f.Inbox[:i], f.Inbox[i+1:]...)
			return nil
		}
	}
	return errors.New("reminder not found: " + id)
}
