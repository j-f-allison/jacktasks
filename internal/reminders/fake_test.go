package reminders

import (
	"context"
	"errors"
	"testing"
)

func TestFakeListInbox(t *testing.T) {
	f := &Fake{
		Inbox: []Reminder{
			{ID: "r1", Title: "email Sarah"},
			{ID: "r2", Title: "look up that thing"},
		},
	}
	items, err := f.ListInbox(context.Background())
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "email Sarah" {
		t.Errorf("items[0].Title = %q", items[0].Title)
	}
}

func TestFakeAdd(t *testing.T) {
	f := &Fake{}
	id, err := f.Add(context.Background(), "buy milk")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	items, _ := f.ListInbox(context.Background())
	if len(items) != 1 || items[0].Title != "buy milk" {
		t.Errorf("unexpected inbox state: %v", items)
	}
}

func TestFakeComplete(t *testing.T) {
	f := &Fake{
		Inbox: []Reminder{{ID: "r1", Title: "task"}},
	}
	if err := f.Complete(context.Background(), "r1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	items, _ := f.ListInbox(context.Background())
	if len(items) != 0 {
		t.Errorf("expected empty inbox after complete, got %d items", len(items))
	}
}

func TestFakeCompleteNotFound(t *testing.T) {
	f := &Fake{}
	err := f.Complete(context.Background(), "no-such-id")
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}

func TestFakeLists(t *testing.T) {
	f := &Fake{AllLists: []string{"Thesis", "Shopping"}}
	lists, err := f.Lists(context.Background())
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}
	if len(lists) != 2 || lists[0] != "Thesis" || lists[1] != "Shopping" {
		t.Errorf("Lists = %v", lists)
	}
}

func TestFakeListsErr(t *testing.T) {
	f := &Fake{ListsErr: errors.New("no permission")}
	if _, err := f.Lists(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFakeListItems(t *testing.T) {
	f := &Fake{
		ItemsByList: map[string][]Reminder{
			"Thesis": {{ID: "t1", Title: "Read Ch. 7"}, {ID: "t2", Title: "Write outline"}},
		},
	}
	items, err := f.ListItems(context.Background(), "Thesis")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 2 || items[0].Title != "Read Ch. 7" {
		t.Errorf("ListItems = %v", items)
	}
}

func TestFakeListItemsInboxDelegatesToInbox(t *testing.T) {
	f := &Fake{
		Inbox: []Reminder{{ID: "i1", Title: "inbox item"}},
	}
	items, err := f.ListItems(context.Background(), InboxListName)
	if err != nil {
		t.Fatalf("ListItems inbox: %v", err)
	}
	if len(items) != 1 || items[0].Title != "inbox item" {
		t.Errorf("expected inbox item, got %v", items)
	}
}
