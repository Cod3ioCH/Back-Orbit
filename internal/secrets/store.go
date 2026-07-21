// Package secrets stores credentials encrypted at rest: repository
// passwords, database credentials, notification tokens and project secrets.
//
// The store follows a two-layer key hierarchy (see
// docs/adr/0004-secret-store-crypto.md):
//
//	master passphrase --Argon2id--> key-encryption key (KEK)
//	                                     |
//	                                     v  wraps
//	                            data encryption key (DEK)
//	                                     |
//	                                     v  encrypts
//	                              individual secrets
//
// The passphrase is never stored. Changing it rewraps the DEK only, which is
// one write; rotating the DEK re-encrypts every secret. Keeping those two
// operations separate is the entire point of the indirection.
package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cod3ioCH/Back-Orbit/internal/crypto"
)

// Kind classifies what a secret is for. Callers address secrets by kind and
// name, which keeps identifiers stable and meaningful across rotations.
type Kind string

const (
	KindSystem       Kind = "system"
	KindRepository   Kind = "repository"
	KindDatabase     Kind = "database"
	KindNotification Kind = "notification"
	KindProject      Kind = "project"
)

// Valid reports whether k is a known kind. Kinds come from API input, so they
// are validated rather than trusted.
func (k Kind) Valid() bool {
	switch k {
	case KindSystem, KindRepository, KindDatabase, KindNotification, KindProject:
		return true
	default:
		return false
	}
}

var (
	// ErrLocked means the store holds no data encryption key right now, so no
	// secret can be read or written.
	ErrLocked = errors.New("secrets: store is locked")
	// ErrNotInitialized means no master passphrase has been set yet.
	ErrNotInitialized = errors.New("secrets: store is not initialised")
	// ErrAlreadyInitialized means initialisation was attempted twice.
	ErrAlreadyInitialized = errors.New("secrets: store is already initialised")
	// ErrInvalidPassphrase means the passphrase did not unwrap the key.
	ErrInvalidPassphrase = errors.New("secrets: invalid master passphrase")
	// ErrNotFound means no secret exists under that kind and name.
	ErrNotFound = errors.New("secrets: secret not found")
	// ErrInvalidKind means the secret kind is not one of the known kinds.
	ErrInvalidKind = errors.New("secrets: invalid secret kind")
)

// minimumPassphraseLength is enforced on the master passphrase. Everything in
// the store is only as strong as this value, and unlike a login password it
// cannot be rate-limited by the server once a database has been stolen.
const minimumPassphraseLength = 12

// Metadata describes a stored secret without exposing its value. This is the
// only shape the API layer ever sees: there is no field here that could carry
// a plaintext secret into a response body by accident.
type Metadata struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	Name       string    `json:"name"`
	KeyVersion int       `json:"keyVersion"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Store is the encrypted secret store. It is safe for concurrent use.
type Store struct {
	db *sql.DB

	mu sync.RWMutex
	// dek is the unwrapped data encryption key. A nil dek means locked; it is
	// the single piece of state that decides whether secrets are readable.
	dek []byte
	// keyVersion is the generation of the currently loaded dek.
	keyVersion int
}

// NewStore creates a store backed by db. The store starts locked.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// IsInitialized reports whether a master passphrase has ever been set.
func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_store`).Scan(&count); err != nil {
		return false, fmt.Errorf("secrets: check initialisation: %w", err)
	}
	return count > 0, nil
}

// IsUnlocked reports whether the store currently holds a usable key.
func (s *Store) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dek != nil
}

// Initialize sets the master passphrase for a fresh installation, generates
// the data encryption key, and leaves the store unlocked.
func (s *Store) Initialize(ctx context.Context, passphrase string) error {
	if len(passphrase) < minimumPassphraseLength {
		return fmt.Errorf("secrets: master passphrase must be at least %d characters", minimumPassphraseLength)
	}

	initialized, err := s.IsInitialized(ctx)
	if err != nil {
		return err
	}
	if initialized {
		return ErrAlreadyInitialized
	}

	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultKDFParams()

	kek, err := crypto.DeriveKey(passphrase, salt, params)
	if err != nil {
		return err
	}
	defer crypto.Zero(kek)

	dek, err := crypto.NewKey()
	if err != nil {
		return err
	}

	wrapped, nonce, err := crypto.Seal(kek, dek, dekAssociatedData(1))
	if err != nil {
		crypto.Zero(dek)
		return err
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO secret_store
			(id, kdf_salt, kdf_time_cost, kdf_memory_kib, kdf_parallelism,
			 wrapped_dek, wrapped_dek_nonce, key_version, created_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		salt, params.TimeCost, params.MemoryKiB, params.Parallelism,
		wrapped, nonce, formatTime(now), formatTime(now),
	)
	if err != nil {
		crypto.Zero(dek)
		return fmt.Errorf("secrets: persist key material: %w", err)
	}

	s.mu.Lock()
	s.dek = dek
	s.keyVersion = 1
	s.mu.Unlock()

	return nil
}

// Unlock derives the key-encryption key from passphrase and unwraps the data
// encryption key. It is idempotent: unlocking an unlocked store with the
// correct passphrase simply succeeds.
func (s *Store) Unlock(ctx context.Context, passphrase string) error {
	record, err := s.loadKeyRecord(ctx)
	if err != nil {
		return err
	}

	kek, err := crypto.DeriveKey(passphrase, record.salt, record.params)
	if err != nil {
		return err
	}
	defer crypto.Zero(kek)

	dek, err := crypto.Open(kek, record.wrappedDEK, record.wrappedDEKNonce, dekAssociatedData(record.keyVersion))
	if err != nil {
		// The AEAD cannot distinguish a wrong passphrase from tampered key
		// material, and neither should the caller: reporting anything more
		// specific would turn this into an oracle.
		return ErrInvalidPassphrase
	}

	s.mu.Lock()
	previous := s.dek
	s.dek = dek
	s.keyVersion = record.keyVersion
	s.mu.Unlock()

	if previous != nil {
		crypto.Zero(previous)
	}
	return nil
}

// Lock discards the data encryption key. Secrets stay on disk, encrypted, and
// become unreadable until the next unlock.
func (s *Store) Lock() {
	s.mu.Lock()
	dek := s.dek
	s.dek = nil
	s.keyVersion = 0
	s.mu.Unlock()

	if dek != nil {
		crypto.Zero(dek)
	}
}

// Put stores or replaces the secret under kind and name.
func (s *Store) Put(ctx context.Context, kind Kind, name, value string) (Metadata, error) {
	if !kind.Valid() {
		return Metadata{}, ErrInvalidKind
	}
	if name == "" {
		return Metadata{}, errors.New("secrets: name must not be empty")
	}

	dek, keyVersion, err := s.currentKey()
	if err != nil {
		return Metadata{}, err
	}
	defer crypto.Zero(dek)

	existing, err := s.metadata(ctx, kind, name)
	switch {
	case err == nil:
		// Keep the identity stable across updates so anything referencing this
		// secret keeps resolving, and so the associated data stays valid.
		return s.write(ctx, existing.ID, kind, name, value, dek, keyVersion, existing.CreatedAt)
	case errors.Is(err, ErrNotFound):
		return s.write(ctx, uuid.NewString(), kind, name, value, dek, keyVersion, time.Now().UTC())
	default:
		return Metadata{}, err
	}
}

// Get returns the plaintext value of a secret.
//
// This is the only function in the codebase that hands out a decrypted
// secret. Callers are expected to use it just in time — passing a repository
// password to the backup engine, for instance — and never to place the result
// in an API response, a log line or an audit event.
func (s *Store) Get(ctx context.Context, kind Kind, name string) (string, error) {
	dek, _, err := s.currentKey()
	if err != nil {
		return "", err
	}
	defer crypto.Zero(dek)

	var (
		id         string
		ciphertext []byte
		nonce      []byte
		keyVersion int
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT id, ciphertext, nonce, key_version FROM secrets WHERE type = ? AND name = ?`,
		string(kind), name,
	).Scan(&id, &ciphertext, &nonce, &keyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secrets: read secret: %w", err)
	}

	plaintext, err := crypto.Open(dek, ciphertext, nonce, secretAssociatedData(id, kind, name))
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt %s/%s: %w", kind, name, err)
	}
	return string(plaintext), nil
}

// Delete removes a secret. Deleting a secret that does not exist is not an
// error, so callers can clean up idempotently.
func (s *Store) Delete(ctx context.Context, kind Kind, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE type = ? AND name = ?`, string(kind), name)
	if err != nil {
		return fmt.Errorf("secrets: delete secret: %w", err)
	}
	return nil
}

// List returns metadata for every stored secret, never their values. It works
// while the store is locked, so an operator can always see what exists.
func (s *Store) List(ctx context.Context) ([]Metadata, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, key_version, created_at, updated_at
		 FROM secrets ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("secrets: list secrets: %w", err)
	}
	defer rows.Close()

	result := []Metadata{}
	for rows.Next() {
		var (
			m                    Metadata
			kind                 string
			createdAt, updatedAt string
		)
		if err := rows.Scan(&m.ID, &kind, &m.Name, &m.KeyVersion, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("secrets: scan secret: %w", err)
		}
		m.Kind = Kind(kind)
		m.CreatedAt, _ = parseTime(createdAt)
		m.UpdatedAt, _ = parseTime(updatedAt)
		result = append(result, m)
	}
	return result, rows.Err()
}

// ChangeMasterPassphrase rewraps the data encryption key under a new
// passphrase. Secrets are untouched, so this stays a single write no matter
// how many secrets exist.
func (s *Store) ChangeMasterPassphrase(ctx context.Context, current, next string) error {
	if len(next) < minimumPassphraseLength {
		return fmt.Errorf("secrets: master passphrase must be at least %d characters", minimumPassphraseLength)
	}

	record, err := s.loadKeyRecord(ctx)
	if err != nil {
		return err
	}

	currentKEK, err := crypto.DeriveKey(current, record.salt, record.params)
	if err != nil {
		return err
	}
	defer crypto.Zero(currentKEK)

	dek, err := crypto.Open(currentKEK, record.wrappedDEK, record.wrappedDEKNonce, dekAssociatedData(record.keyVersion))
	if err != nil {
		return ErrInvalidPassphrase
	}
	defer crypto.Zero(dek)

	// A new salt with the new passphrase, so the old derivation is worthless
	// even to someone holding an older copy of the database.
	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	params := crypto.DefaultKDFParams()

	newKEK, err := crypto.DeriveKey(next, salt, params)
	if err != nil {
		return err
	}
	defer crypto.Zero(newKEK)

	wrapped, nonce, err := crypto.Seal(newKEK, dek, dekAssociatedData(record.keyVersion))
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE secret_store
		   SET kdf_salt = ?, kdf_time_cost = ?, kdf_memory_kib = ?, kdf_parallelism = ?,
		       wrapped_dek = ?, wrapped_dek_nonce = ?, updated_at = ?
		 WHERE id = 1`,
		salt, params.TimeCost, params.MemoryKiB, params.Parallelism,
		wrapped, nonce, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("secrets: rewrap key: %w", err)
	}
	return nil
}

// RotateDataKey generates a new data encryption key and re-encrypts every
// secret under it, in a single transaction. A failure part-way through leaves
// the store exactly as it was rather than half-rotated and unreadable.
func (s *Store) RotateDataKey(ctx context.Context, passphrase string) error {
	record, err := s.loadKeyRecord(ctx)
	if err != nil {
		return err
	}

	kek, err := crypto.DeriveKey(passphrase, record.salt, record.params)
	if err != nil {
		return err
	}
	defer crypto.Zero(kek)

	oldDEK, err := crypto.Open(kek, record.wrappedDEK, record.wrappedDEKNonce, dekAssociatedData(record.keyVersion))
	if err != nil {
		return ErrInvalidPassphrase
	}
	defer crypto.Zero(oldDEK)

	newDEK, err := crypto.NewKey()
	if err != nil {
		return err
	}
	newVersion := record.keyVersion + 1

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		crypto.Zero(newDEK)
		return fmt.Errorf("secrets: begin rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT id, type, name, ciphertext, nonce FROM secrets`)
	if err != nil {
		crypto.Zero(newDEK)
		return fmt.Errorf("secrets: read secrets for rotation: %w", err)
	}

	type reencrypted struct {
		id         string
		ciphertext []byte
		nonce      []byte
	}
	var updates []reencrypted

	for rows.Next() {
		var (
			id, kind, name string
			ciphertext     []byte
			nonce          []byte
		)
		if err := rows.Scan(&id, &kind, &name, &ciphertext, &nonce); err != nil {
			rows.Close()
			crypto.Zero(newDEK)
			return fmt.Errorf("secrets: scan secret for rotation: %w", err)
		}

		aad := secretAssociatedData(id, Kind(kind), name)
		plaintext, err := crypto.Open(oldDEK, ciphertext, nonce, aad)
		if err != nil {
			rows.Close()
			crypto.Zero(newDEK)
			return fmt.Errorf("secrets: decrypt %s/%s during rotation: %w", kind, name, err)
		}

		newCiphertext, newNonce, err := crypto.Seal(newDEK, plaintext, aad)
		crypto.Zero(plaintext)
		if err != nil {
			rows.Close()
			crypto.Zero(newDEK)
			return err
		}
		updates = append(updates, reencrypted{id: id, ciphertext: newCiphertext, nonce: newNonce})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		crypto.Zero(newDEK)
		return fmt.Errorf("secrets: iterate secrets for rotation: %w", err)
	}

	now := formatTime(time.Now().UTC())
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx,
			`UPDATE secrets SET ciphertext = ?, nonce = ?, key_version = ?, updated_at = ? WHERE id = ?`,
			update.ciphertext, update.nonce, newVersion, now, update.id,
		); err != nil {
			crypto.Zero(newDEK)
			return fmt.Errorf("secrets: rewrite secret during rotation: %w", err)
		}
	}

	wrapped, wrappedNonce, err := crypto.Seal(kek, newDEK, dekAssociatedData(newVersion))
	if err != nil {
		crypto.Zero(newDEK)
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE secret_store SET wrapped_dek = ?, wrapped_dek_nonce = ?, key_version = ?, updated_at = ? WHERE id = 1`,
		wrapped, wrappedNonce, newVersion, now,
	); err != nil {
		crypto.Zero(newDEK)
		return fmt.Errorf("secrets: store rotated key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		crypto.Zero(newDEK)
		return fmt.Errorf("secrets: commit rotation: %w", err)
	}

	s.mu.Lock()
	previous := s.dek
	s.dek = newDEK
	s.keyVersion = newVersion
	s.mu.Unlock()
	if previous != nil {
		crypto.Zero(previous)
	}

	return nil
}

// currentKey returns a copy of the data encryption key. A copy is handed out
// so callers can zero it without racing another goroutine that is mid-use.
func (s *Store) currentKey() ([]byte, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.dek == nil {
		return nil, 0, ErrLocked
	}
	key := make([]byte, len(s.dek))
	copy(key, s.dek)
	return key, s.keyVersion, nil
}

func (s *Store) write(ctx context.Context, id string, kind Kind, name, value string,
	dek []byte, keyVersion int, createdAt time.Time) (Metadata, error) {

	ciphertext, nonce, err := crypto.Seal(dek, []byte(value), secretAssociatedData(id, kind, name))
	if err != nil {
		return Metadata{}, err
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO secrets (id, type, name, ciphertext, nonce, key_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			ciphertext = excluded.ciphertext,
			nonce = excluded.nonce,
			key_version = excluded.key_version,
			updated_at = excluded.updated_at`,
		id, string(kind), name, ciphertext, nonce, keyVersion,
		formatTime(createdAt), formatTime(now),
	)
	if err != nil {
		return Metadata{}, fmt.Errorf("secrets: write secret: %w", err)
	}

	return Metadata{
		ID:         id,
		Kind:       kind,
		Name:       name,
		KeyVersion: keyVersion,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}, nil
}

func (s *Store) metadata(ctx context.Context, kind Kind, name string) (Metadata, error) {
	var (
		m                    Metadata
		createdAt, updatedAt string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, key_version, created_at, updated_at FROM secrets WHERE type = ? AND name = ?`,
		string(kind), name,
	).Scan(&m.ID, &m.KeyVersion, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("secrets: read secret metadata: %w", err)
	}

	m.Kind = kind
	m.Name = name
	m.CreatedAt, _ = parseTime(createdAt)
	m.UpdatedAt, _ = parseTime(updatedAt)
	return m, nil
}

type keyRecord struct {
	salt            []byte
	params          crypto.KDFParams
	wrappedDEK      []byte
	wrappedDEKNonce []byte
	keyVersion      int
}

func (s *Store) loadKeyRecord(ctx context.Context) (keyRecord, error) {
	var record keyRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT kdf_salt, kdf_time_cost, kdf_memory_kib, kdf_parallelism,
		       wrapped_dek, wrapped_dek_nonce, key_version
		  FROM secret_store WHERE id = 1`,
	).Scan(&record.salt, &record.params.TimeCost, &record.params.MemoryKiB,
		&record.params.Parallelism, &record.wrappedDEK, &record.wrappedDEKNonce, &record.keyVersion)

	if errors.Is(err, sql.ErrNoRows) {
		return keyRecord{}, ErrNotInitialized
	}
	if err != nil {
		return keyRecord{}, fmt.Errorf("secrets: read key material: %w", err)
	}
	return record, nil
}

// dekAssociatedData binds the wrapped data key to its generation, so an old
// wrapped key cannot be replayed over a newer one after a rotation.
func dekAssociatedData(keyVersion int) []byte {
	return []byte(fmt.Sprintf("back-orbit/dek/v%d", keyVersion))
}

// secretAssociatedData binds a ciphertext to the exact record it belongs to.
// Without it, someone with write access to the database could move one
// secret's ciphertext onto another row and have it decrypt cleanly — quietly
// swapping, say, a staging repository password in for production.
func secretAssociatedData(id string, kind Kind, name string) []byte {
	return []byte(fmt.Sprintf("back-orbit/secret/%s/%s/%s", id, kind, name))
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}
