package auth

import (
	"context"
	"testing"
	"time"

	"github.com/back-orbit/back-orbit/internal/dbtest"
)

func TestSessionCreateAndValidate(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	sessions := NewSessionStore(db, time.Hour)
	ctx := context.Background()

	user, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	session, err := sessions.Create(ctx, user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if session.Token == "" {
		t.Fatal("expected a non-empty session token")
	}

	validated, err := sessions.Validate(ctx, session.Token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if validated.UserID != user.ID {
		t.Fatalf("expected user ID %q, got %q", user.ID, validated.UserID)
	}
}

func TestSessionValidateRejectsUnknownToken(t *testing.T) {
	db := dbtest.Open(t)
	sessions := NewSessionStore(db, time.Hour)

	_, err := sessions.Validate(context.Background(), "does-not-exist")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionValidateRejectsExpiredToken(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	sessions := NewSessionStore(db, -time.Second) // already expired on creation
	ctx := context.Background()

	user, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	session, err := sessions.Create(ctx, user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	_, err = sessions.Validate(ctx, session.Token)
	if err != ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestSessionDelete(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	sessions := NewSessionStore(db, time.Hour)
	ctx := context.Background()

	user, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	session, err := sessions.Create(ctx, user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	if err := sessions.Delete(ctx, session.Token); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = sessions.Validate(ctx, session.Token)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

func TestSessionTokenNotStoredInPlaintext(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	sessions := NewSessionStore(db, time.Hour)
	ctx := context.Background()

	user, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	session, err := sessions.Create(ctx, user.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	var storedTokenHash string
	err = db.QueryRowContext(ctx, `SELECT token_hash FROM sessions WHERE id = ?`, session.ID).Scan(&storedTokenHash)
	if err != nil {
		t.Fatalf("query stored session: %v", err)
	}
	if storedTokenHash == session.Token {
		t.Fatal("raw session token must never be stored in the database")
	}
}
