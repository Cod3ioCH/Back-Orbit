// Package crypto holds Back-Orbit's cryptographic primitives: Argon2id key
// derivation and XChaCha20-Poly1305 authenticated encryption.
//
// Nothing here invents cryptography. It is a thin, opinionated wrapper around
// golang.org/x/crypto that removes the choices most easily got wrong — nonce
// generation, nonce reuse, unauthenticated ciphertext, and forgetting to bind
// a ciphertext to its context. See docs/adr/0004-secret-store-crypto.md.
package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// KeySize is the length of every symmetric key used here.
	KeySize = chacha20poly1305.KeySize // 32 bytes
	// NonceSize is XChaCha20-Poly1305's nonce length.
	//
	// The extended (X) variant is chosen precisely because its 192-bit nonce
	// is large enough that independently generated random nonces will not
	// collide in any realistic number of encryptions. That removes the need
	// for a counter, and with it the classic failure of reusing a nonce after
	// a restore or a rollback of the database.
	NonceSize = chacha20poly1305.NonceSizeX // 24 bytes
	// SaltSize is the length of an Argon2id salt.
	SaltSize = 16
)

// ErrDecrypt is returned whenever a ciphertext fails to authenticate. It is
// deliberately indistinguishable between "wrong key" and "tampered data":
// telling those apart would hand an attacker a probe, and callers that need
// to explain a failure to a user can do so from context.
var ErrDecrypt = errors.New("crypto: decryption failed")

// KDFParams are the Argon2id cost parameters. They are stored alongside the
// data they protected so that raising the defaults later does not lock anyone
// out of secrets derived with the old ones.
type KDFParams struct {
	// TimeCost is the number of passes.
	TimeCost uint32
	// MemoryKiB is the memory cost in kibibytes.
	MemoryKiB uint32
	// Parallelism is the number of lanes.
	Parallelism uint8
}

// DefaultKDFParams are used when deriving a new key. These are deliberately
// far more expensive than the parameters used for login password hashing:
// this derivation runs once per unlock, not on every sign-in, so the cost is
// paid rarely and buys meaningful resistance to offline attack on a stolen
// database.
func DefaultKDFParams() KDFParams {
	return KDFParams{
		TimeCost:    4,
		MemoryKiB:   256 * 1024, // 256 MiB
		Parallelism: 4,
	}
}

// Validate rejects parameters that would produce a weak key. Parameters are
// read back from the database, so they are treated as untrusted input.
func (p KDFParams) Validate() error {
	switch {
	case p.TimeCost < 1:
		return errors.New("crypto: argon2 time cost must be at least 1")
	case p.MemoryKiB < 8*1024:
		return errors.New("crypto: argon2 memory cost must be at least 8 MiB")
	case p.Parallelism < 1:
		return errors.New("crypto: argon2 parallelism must be at least 1")
	}
	return nil
}

// DeriveKey turns a passphrase into a KeySize key using Argon2id.
func DeriveKey(passphrase string, salt []byte, params KDFParams) ([]byte, error) {
	if len(salt) < SaltSize {
		return nil, fmt.Errorf("crypto: salt must be at least %d bytes", SaltSize)
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	return argon2.IDKey(
		[]byte(passphrase),
		salt,
		params.TimeCost,
		params.MemoryKiB,
		params.Parallelism,
		KeySize,
	), nil
}

// NewSalt returns a fresh random salt.
func NewSalt() ([]byte, error) {
	return randomBytes(SaltSize)
}

// NewKey returns a fresh random symmetric key, for use as a data encryption
// key.
func NewKey() ([]byte, error) {
	return randomBytes(KeySize)
}

// Seal encrypts plaintext with key and returns the ciphertext and the nonce
// used.
//
// associatedData is authenticated but not encrypted. Callers should pass
// something that identifies where this ciphertext belongs — Back-Orbit passes
// the secret's identity — so that an attacker who can write to the database
// cannot move a ciphertext from one record to another. Such a swap would
// otherwise decrypt cleanly, silently substituting, say, a test repository's
// password for a production one.
func Seal(key, plaintext, associatedData []byte) (ciphertext, nonce []byte, err error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, nil, err
	}

	nonce, err = randomBytes(NonceSize)
	if err != nil {
		return nil, nil, err
	}

	ciphertext = aead.Seal(nil, nonce, plaintext, associatedData)
	return ciphertext, nonce, nil
}

// Open decrypts a ciphertext produced by Seal. It returns ErrDecrypt if the
// key is wrong, the nonce does not match, the ciphertext was modified, or the
// associated data differs from the one it was sealed with.
func Open(key, ciphertext, nonce, associatedData []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceSize {
		return nil, ErrDecrypt
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func newAEAD(key []byte) (aead interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}, err error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: key must be %d bytes, got %d", KeySize, len(key))
	}
	return chacha20poly1305.NewX(key)
}

// Zero overwrites b. Go gives no guarantee that a value was not already copied
// elsewhere by the garbage collector, so this is a best-effort measure that
// shortens how long key material stays readable in memory — worth doing, but
// not something to rely on as the only protection.
func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("crypto: read random bytes: %w", err)
	}
	return b, nil
}
