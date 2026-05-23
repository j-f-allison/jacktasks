package store

import (
	"context"
	"errors"
	"testing"
)

func TestSetGetConfig(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetConfig(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key err = %v, want ErrNotFound", err)
	}

	if err := s.SetConfig(ctx, "foo", "bar"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := s.GetConfig(ctx, "foo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != "bar" {
		t.Errorf("got %q, want %q", v, "bar")
	}

	// Overwrite.
	if err := s.SetConfig(ctx, "foo", "baz"); err != nil {
		t.Fatalf("update: %v", err)
	}
	v, _ = s.GetConfig(ctx, "foo")
	if v != "baz" {
		t.Errorf("after overwrite got %q, want %q", v, "baz")
	}
}

func TestDeviceIDLazyInit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id1, err := s.DeviceID(ctx)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if id1 == "" {
		t.Fatal("empty device id")
	}

	id2, err := s.DeviceID(ctx)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id2 != id1 {
		t.Errorf("device id changed: %q vs %q", id1, id2)
	}

	stored, err := s.GetConfig(ctx, "device_id")
	if err != nil {
		t.Fatalf("get device_id: %v", err)
	}
	if stored != id1 {
		t.Errorf("config mismatch: %q vs %q", stored, id1)
	}
}