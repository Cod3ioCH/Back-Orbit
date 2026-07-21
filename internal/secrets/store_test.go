package secrets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/dbtest"
)

const testPassphrase = "a-sufficiently-long-master-passphrase"

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := dbtest.Open(t)
	return NewStore(db), db
}

func newInitializedStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	store, db := newStore(t)
	if err := store.Initialize(context.Background(), testPassphrase); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return store, db
}

func TestInitializeUnlocksTheStore(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	if store.IsUnlocked() {
		t.Fatal("a fresh store must start locked")
	}
	initialized, err := store.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized: %v", err)
	}
	if initialized {
		t.Fatal("a fresh store must not report itself as initialised")
	}

	if err := store.Initialize(ctx, testPassphrase); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !store.IsUnlocked() {
		t.Fatal("the store must be unlocked right after initialisation")
	}
}

func TestInitializeRejectsShortPassphrase(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Initialize(context.Background(), "short"); err == nil {
		t.Fatal("expected a short master passphrase to be rejected")
	}
}

func TestInitializeTwiceIsRefused(t *testing.T) {
	store, _ := newInitializedStore(t)
	if err := store.Initialize(context.Background(), testPassphrase); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("expected ErrAlreadyInitialized, got %v", err)
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	const value = "correct horse battery staple"
	meta, err := store.Put(ctx, KindRepository, "primary", value)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if meta.ID == "" || meta.Kind != KindRepository || meta.Name != "primary" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	got, err := store.Get(ctx, KindRepository, "primary")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != value {
		t.Fatalf("Get = %q, want %q", got, value)
	}
}

// TestSecretsAreNotStoredInPlaintext inspects the database directly. This is
// the whole promise of the package: whatever ends up on disk must be useless
// to someone who reads the file.
func TestSecretsAreNotStoredInPlaintext(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	const value = "unmistakable-plaintext-marker-9f3a"
	if _, err := store.Put(ctx, KindDatabase, "postgres", value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var ciphertext []byte
	if err := db.QueryRowContext(ctx, `SELECT ciphertext FROM secrets WHERE name = 'postgres'`).
		Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if strings.Contains(string(ciphertext), value) {
		t.Fatal("the secret value is readable in the stored ciphertext")
	}

	// Nor may it appear anywhere else in the row.
	var kind, name string
	if err := db.QueryRowContext(ctx, `SELECT type, name FROM secrets WHERE name = 'postgres'`).
		Scan(&kind, &name); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if strings.Contains(kind+name, value) {
		t.Fatal("the secret value leaked into an unencrypted column")
	}
}

// TestMasterPassphraseIsNeverStored guards the central claim of the design.
func TestMasterPassphraseIsNeverStored(t *testing.T) {
	store, db := newInitializedStore(t)
	if _, err := store.Put(context.Background(), KindSystem, "anything", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var salt, wrapped []byte
	if err := db.QueryRow(`SELECT kdf_salt, wrapped_dek FROM secret_store WHERE id = 1`).
		Scan(&salt, &wrapped); err != nil {
		t.Fatalf("read key record: %v", err)
	}

	for _, blob := range [][]byte{salt, wrapped} {
		if strings.Contains(string(blob), testPassphrase) {
			t.Fatal("the master passphrase is recoverable from stored key material")
		}
	}
}

func TestLockedStoreRefusesReadsAndWrites(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "primary", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	store.Lock()
	if store.IsUnlocked() {
		t.Fatal("the store must report itself as locked after Lock")
	}

	if _, err := store.Get(ctx, KindRepository, "primary"); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked reading from a locked store, got %v", err)
	}
	if _, err := store.Put(ctx, KindRepository, "other", "value"); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked writing to a locked store, got %v", err)
	}
}

// TestUnlockRestoresAccess covers the restart path: the key lives only in
// memory, so everything must come back from the passphrase alone.
func TestUnlockRestoresAccess(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	const value = "survives a restart"
	if _, err := store.Put(ctx, KindRepository, "primary", value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A brand-new Store over the same database is exactly what a restart
	// produces.
	restarted := NewStore(db)
	if restarted.IsUnlocked() {
		t.Fatal("a store must come up locked after a restart")
	}
	if err := restarted.Unlock(ctx, testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	got, err := restarted.Get(ctx, KindRepository, "primary")
	if err != nil {
		t.Fatalf("Get after unlock: %v", err)
	}
	if got != value {
		t.Fatalf("Get = %q, want %q", got, value)
	}
}

func TestUnlockRejectsWrongPassphrase(t *testing.T) {
	store, db := newInitializedStore(t)
	store.Lock()

	restarted := NewStore(db)
	if err := restarted.Unlock(context.Background(), "not-the-master-passphrase"); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
	if restarted.IsUnlocked() {
		t.Fatal("a failed unlock must leave the store locked")
	}
}

func TestUnlockOnUninitializedStore(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Unlock(context.Background(), testPassphrase); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized, got %v", err)
	}
}

func TestPutReplacesValueAndKeepsIdentity(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	first, err := store.Put(ctx, KindRepository, "primary", "old value")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	second, err := store.Put(ctx, KindRepository, "primary", "new value")
	if err != nil {
		t.Fatalf("Put (update): %v", err)
	}

	if first.ID != second.ID {
		t.Fatal("updating a secret must keep its identity stable")
	}

	got, err := store.Get(ctx, KindRepository, "primary")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "new value" {
		t.Fatalf("Get = %q, want the updated value", got)
	}
}

func TestGetMissingSecret(t *testing.T) {
	store, _ := newInitializedStore(t)
	if _, err := store.Get(context.Background(), KindRepository, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteRemovesSecret(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "primary", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, KindRepository, "primary"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, KindRepository, "primary"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the secret to be gone, got %v", err)
	}

	// Deleting again must stay quiet so cleanup can be idempotent.
	if err := store.Delete(ctx, KindRepository, "primary"); err != nil {
		t.Fatalf("deleting a missing secret should not error: %v", err)
	}
}

// TestListNeverExposesValues checks the shape of the type callers get: there
// must be no route from List to a plaintext secret.
func TestListNeverExposesValues(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	const value = "must-not-appear-in-listings"
	if _, err := store.Put(ctx, KindNotification, "gotify", value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(list))
	}
	if got := list[0]; got.Kind != KindNotification || got.Name != "gotify" {
		t.Fatalf("unexpected metadata: %+v", got)
	}

	// The rendered metadata must not contain the value anywhere.
	if strings.Contains(strings.Join([]string{list[0].ID, string(list[0].Kind), list[0].Name}, "|"), value) {
		t.Fatal("a secret value appeared in listing metadata")
	}
}

func TestListWorksWhileLocked(t *testing.T) {
	store, _ := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "primary", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	store.Lock()

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List while locked: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("an operator must still see what exists while locked, got %d", len(list))
	}
}

func TestPutRejectsUnknownKind(t *testing.T) {
	store, _ := newInitializedStore(t)
	if _, err := store.Put(context.Background(), Kind("made-up"), "x", "v"); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("expected ErrInvalidKind, got %v", err)
	}
}

// TestChangeMasterPassphrase proves the indirection pays off: the old
// passphrase stops working, the new one works, and secrets are untouched.
func TestChangeMasterPassphrase(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	const value = "unchanged by a passphrase change"
	if _, err := store.Put(ctx, KindRepository, "primary", value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	const newPassphrase = "an-entirely-different-master-passphrase"
	if err := store.ChangeMasterPassphrase(ctx, testPassphrase, newPassphrase); err != nil {
		t.Fatalf("ChangeMasterPassphrase: %v", err)
	}

	restarted := NewStore(db)
	if err := restarted.Unlock(ctx, testPassphrase); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("the old passphrase must stop working, got %v", err)
	}
	if err := restarted.Unlock(ctx, newPassphrase); err != nil {
		t.Fatalf("Unlock with the new passphrase: %v", err)
	}

	got, err := restarted.Get(ctx, KindRepository, "primary")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != value {
		t.Fatalf("secret changed across a passphrase change: %q", got)
	}
}

func TestChangeMasterPassphraseRejectsWrongCurrent(t *testing.T) {
	store, _ := newInitializedStore(t)
	err := store.ChangeMasterPassphrase(context.Background(), "wrong-current-passphrase", "a-new-long-passphrase")
	if !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

// TestRotateDataKeyReencryptsEverything is the test that makes key rotation
// trustworthy: every secret must still decrypt afterwards, and every row must
// carry the new key generation.
func TestRotateDataKeyReencryptsEverything(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	values := map[string]string{
		"primary":   "first repository password",
		"secondary": "second repository password",
	}
	for name, value := range values {
		if _, err := store.Put(ctx, KindRepository, name, value); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}

	var beforeCiphertext []byte
	if err := db.QueryRow(`SELECT ciphertext FROM secrets WHERE name = 'primary'`).Scan(&beforeCiphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}

	if err := store.RotateDataKey(ctx, testPassphrase); err != nil {
		t.Fatalf("RotateDataKey: %v", err)
	}

	for name, want := range values {
		got, err := store.Get(ctx, KindRepository, name)
		if err != nil {
			t.Fatalf("Get %s after rotation: %v", name, err)
		}
		if got != want {
			t.Fatalf("secret %s = %q after rotation, want %q", name, got, want)
		}
	}

	// The stored ciphertext must actually have changed, and the key version
	// must have advanced everywhere — a partially rotated store would be a
	// silent time bomb.
	var afterCiphertext []byte
	var versions int
	if err := db.QueryRow(`SELECT ciphertext FROM secrets WHERE name = 'primary'`).Scan(&afterCiphertext); err != nil {
		t.Fatalf("read ciphertext after rotation: %v", err)
	}
	if string(beforeCiphertext) == string(afterCiphertext) {
		t.Fatal("rotation left the ciphertext unchanged")
	}
	if err := db.QueryRow(`SELECT COUNT(DISTINCT key_version) FROM secrets`).Scan(&versions); err != nil {
		t.Fatalf("count key versions: %v", err)
	}
	if versions != 1 {
		t.Fatalf("expected every secret on one key version after rotation, found %d", versions)
	}

	// And the rotated store must survive a restart.
	restarted := NewStore(db)
	if err := restarted.Unlock(ctx, testPassphrase); err != nil {
		t.Fatalf("Unlock after rotation: %v", err)
	}
	if _, err := restarted.Get(ctx, KindRepository, "primary"); err != nil {
		t.Fatalf("Get after rotation and restart: %v", err)
	}
}

func TestRotateDataKeyRejectsWrongPassphrase(t *testing.T) {
	store, _ := newInitializedStore(t)
	if err := store.RotateDataKey(context.Background(), "wrong"); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

// TestTamperedCiphertextIsDetected simulates someone with write access to the
// database editing a stored secret. It must fail loudly rather than hand back
// something wrong.
func TestTamperedCiphertextIsDetected(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "primary", "original"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var ciphertext []byte
	if err := db.QueryRow(`SELECT ciphertext FROM secrets WHERE name = 'primary'`).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	ciphertext[0] ^= 0xFF
	if _, err := db.Exec(`UPDATE secrets SET ciphertext = ? WHERE name = 'primary'`, ciphertext); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if _, err := store.Get(ctx, KindRepository, "primary"); err == nil {
		t.Fatal("expected reading a tampered secret to fail")
	}
}

// TestSwappedCiphertextIsDetected is the attack the associated-data binding
// exists to stop: moving one secret's ciphertext onto another row. Without the
// binding it would decrypt cleanly and quietly substitute the wrong
// credential — a staging password used against production, say.
func TestSwappedCiphertextIsDetected(t *testing.T) {
	store, db := newInitializedStore(t)
	ctx := context.Background()

	if _, err := store.Put(ctx, KindRepository, "production", "production-password"); err != nil {
		t.Fatalf("Put production: %v", err)
	}
	if _, err := store.Put(ctx, KindRepository, "staging", "staging-password"); err != nil {
		t.Fatalf("Put staging: %v", err)
	}

	var stagingCiphertext, stagingNonce []byte
	if err := db.QueryRow(`SELECT ciphertext, nonce FROM secrets WHERE name = 'staging'`).
		Scan(&stagingCiphertext, &stagingNonce); err != nil {
		t.Fatalf("read staging: %v", err)
	}

	// Overwrite production's ciphertext with staging's.
	if _, err := db.Exec(`UPDATE secrets SET ciphertext = ?, nonce = ? WHERE name = 'production'`,
		stagingCiphertext, stagingNonce); err != nil {
		t.Fatalf("swap: %v", err)
	}

	got, err := store.Get(ctx, KindRepository, "production")
	if err == nil {
		t.Fatalf("a swapped ciphertext decrypted successfully and returned %q", got)
	}
}
