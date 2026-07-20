package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors returned by the auth package. Callers (the API layer) map
// these to the appropriate HTTP status without leaking internal detail.
var (
	ErrUserExists         = errors.New("auth: user already exists")
	ErrUserAlreadySetUp   = errors.New("auth: an administrator account already exists")
	ErrInvalidCredentials = errors.New("auth: invalid username or password")
	ErrSessionNotFound    = errors.New("auth: session not found")
	ErrSessionExpired     = errors.New("auth: session expired")
	ErrRateLimited        = errors.New("auth: too many login attempts, try again later")
)

// User is an application user. Back-Orbit's MVP supports a single local
// administrator account; the model allows for more later without a schema
// change.
type User struct {
	ID        string
	Username  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserStore persists users in SQLite.
type UserStore struct {
	db *sql.DB
}

// NewUserStore creates a UserStore backed by db.
func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

// HasAnyUser reports whether at least one user account exists. The setup
// wizard uses this to decide whether admin creation is still allowed.
func (s *UserStore) HasAnyUser(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

// CreateUser creates a new user with the given username and plaintext
// password (which is hashed before storage). It fails with
// ErrUserAlreadySetUp if any user already exists, since the MVP only
// supports a single administrator created via the setup wizard.
func (s *UserStore) CreateUser(ctx context.Context, username, password string) (User, error) {
	hasUser, err := s.HasAnyUser(ctx)
	if err != nil {
		return User{}, err
	}
	if hasUser {
		return User{}, ErrUserAlreadySetUp
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC()
	user := User{
		ID:        uuid.NewString(),
		Username:  username,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, user.Username, passwordHash, formatTime(user.CreatedAt), formatTime(user.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return user, nil
}

// Authenticate verifies a username/password pair and returns the matching
// user. It performs a password verification even when the username is
// unknown (against a fixed dummy hash) to avoid leaking username existence
// through response timing.
func (s *UserStore) Authenticate(ctx context.Context, username, password string) (User, error) {
	var (
		user         User
		passwordHash string
		createdAt    string
		updatedAt    string
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at, updated_at FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &passwordHash, &createdAt, &updatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		// Run the same expensive hash comparison against a dummy hash so
		// that unknown-username and wrong-password requests take
		// comparable time.
		_, _ = VerifyPassword(dummyPasswordHash, password)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}

	ok, err := VerifyPassword(passwordHash, password)
	if err != nil {
		return User{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return User{}, ErrInvalidCredentials
	}

	user.CreatedAt, _ = parseTime(createdAt)
	user.UpdatedAt, _ = parseTime(updatedAt)

	return user, nil
}

// GetByID loads a user by ID.
func (s *UserStore) GetByID(ctx context.Context, id string) (User, error) {
	var (
		user      User
		createdAt string
		updatedAt string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}
	user.CreatedAt, _ = parseTime(createdAt)
	user.UpdatedAt, _ = parseTime(updatedAt)
	return user, nil
}

// dummyPasswordHash is a valid Argon2id hash of a fixed, never-used
// password, computed once at startup. It exists only to equalize timing for
// unknown-username login attempts so that Authenticate always performs one
// password verification, regardless of whether the username exists.
var dummyPasswordHash = mustHashPassword("back-orbit-timing-equalization-placeholder")

func mustHashPassword(password string) string {
	hash, err := HashPassword(password)
	if err != nil {
		panic(fmt.Sprintf("auth: failed to compute dummy password hash: %v", err))
	}
	return hash
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func isUniqueConstraintErr(err error) bool {
	// modernc.org/sqlite wraps the underlying SQLite error message; matching
	// on the well-known SQLite error text is the most portable check without
	// pulling in driver-specific error types.
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
