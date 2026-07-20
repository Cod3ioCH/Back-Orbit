package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Session represents an authenticated session. Token is only ever populated
// on creation (the caller needs it to set the cookie); it is never read back
// from storage, since only its hash is persisted.
type Session struct {
	ID        string
	UserID    string
	Token     string
	IP        string
	UserAgent string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// SessionStore persists sessions in SQLite, storing only a hash of each
// session token so that a database read alone never yields a usable
// session.
type SessionStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSessionStore creates a SessionStore backed by db, issuing sessions
// valid for ttl.
func NewSessionStore(db *sql.DB, ttl time.Duration) *SessionStore {
	return &SessionStore{db: db, ttl: ttl}
}

// Create issues a new session for userID and returns it, including the raw
// token (present only in this return value, never persisted or logged).
func (s *SessionStore) Create(ctx context.Context, userID, ip, userAgent string) (Session, error) {
	token, err := generateToken()
	if err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}

	now := time.Now().UTC()
	session := Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		Token:     token,
		IP:        ip,
		UserAgent: userAgent,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, ip, user_agent, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, hashToken(token), session.IP, session.UserAgent,
		formatTime(session.ExpiresAt), formatTime(session.CreatedAt),
	)
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}

	return session, nil
}

// Validate looks up a session by its raw token and returns it if it exists
// and has not expired. Expired sessions are deleted as a side effect.
func (s *SessionStore) Validate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrSessionNotFound
	}

	var (
		session   Session
		expiresAt string
		createdAt string
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, ip, user_agent, expires_at, created_at FROM sessions WHERE token_hash = ?`,
		hashToken(token),
	).Scan(&session.ID, &session.UserID, &session.IP, &session.UserAgent, &expiresAt, &createdAt)

	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("query session: %w", err)
	}

	session.ExpiresAt, _ = parseTime(expiresAt)
	session.CreatedAt, _ = parseTime(createdAt)

	if time.Now().UTC().After(session.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, session.ID)
		return Session{}, ErrSessionExpired
	}

	return session, nil
}

// Delete removes a session by its raw token (used on logout).
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes all expired sessions. Intended to be called
// periodically.
func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(time.Now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
