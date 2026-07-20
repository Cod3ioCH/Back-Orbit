package events

import (
	"context"
	"testing"

	"github.com/back-orbit/back-orbit/internal/dbtest"
)

func TestStoreInsertAndListRecent(t *testing.T) {
	db := dbtest.Open(t)
	store := NewStore(db)
	ctx := context.Background()

	if _, err := store.Insert(ctx, Event{Action: ActionLoginSucceeded, Metadata: map[string]any{"username": "admin"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := store.Insert(ctx, Event{Action: ActionLogout}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	// Newest first.
	if got[0].Action != ActionLogout {
		t.Fatalf("expected most recent event first, got %q", got[0].Action)
	}
}

func TestStoreListRecentReturnsEmptySliceNotNil(t *testing.T) {
	db := dbtest.Open(t)
	store := NewStore(db)

	got, err := store.ListRecent(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if got == nil {
		t.Fatal("expected an empty slice, not nil, so JSON serializes to [] rather than null")
	}
}

func TestStoreInsertRedactsSensitiveMetadata(t *testing.T) {
	db := dbtest.Open(t)
	store := NewStore(db)
	ctx := context.Background()

	stored, err := store.Insert(ctx, Event{
		Action: ActionLoginFailed,
		Metadata: map[string]any{
			"username": "admin",
			"password": "should-never-be-persisted",
		},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if stored.Metadata["password"] != "[redacted]" {
		t.Fatalf("expected password to be redacted, got %v", stored.Metadata["password"])
	}

	got, err := store.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(got) != 1 || got[0].Metadata["password"] != "[redacted]" {
		t.Fatalf("expected redacted password to be persisted, got %+v", got)
	}
}
