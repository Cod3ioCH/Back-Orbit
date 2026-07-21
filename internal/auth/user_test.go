package auth

import (
	"context"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
)

func TestCreateUserAndAuthenticate(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	ctx := context.Background()

	hasUser, err := users.HasAnyUser(ctx)
	if err != nil {
		t.Fatalf("HasAnyUser: %v", err)
	}
	if hasUser {
		t.Fatal("expected no users initially")
	}

	created, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	authenticated, err := users.Authenticate(ctx, "admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if authenticated.ID != created.ID {
		t.Fatalf("expected user ID %q, got %q", created.ID, authenticated.ID)
	}
}

func TestCreateUserOnlyAllowsOneAdmin(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	ctx := context.Background()

	if _, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, err := users.CreateUser(ctx, "second-admin", "another-long-password")
	if err != ErrUserAlreadySetUp {
		t.Fatalf("expected ErrUserAlreadySetUp, got %v", err)
	}
}

func TestAuthenticateRejectsWrongPassword(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	ctx := context.Background()

	if _, err := users.CreateUser(ctx, "admin", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, err := users.Authenticate(ctx, "admin", "wrong-password")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticateRejectsUnknownUsername(t *testing.T) {
	db := dbtest.Open(t)
	users := NewUserStore(db)
	ctx := context.Background()

	_, err := users.Authenticate(ctx, "nobody", "whatever-password")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
