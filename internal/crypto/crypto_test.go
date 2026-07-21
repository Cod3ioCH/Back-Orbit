package crypto

import (
	"bytes"
	"errors"
	"testing"
)

// testParams keep the tests fast. Production uses DefaultKDFParams, which are
// deliberately expensive; a test suite that used them would take minutes.
func testParams() KDFParams {
	return KDFParams{TimeCost: 1, MemoryKiB: 8 * 1024, Parallelism: 1}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	plaintext := []byte("a repository password")
	aad := []byte("secret:repository:primary")

	ciphertext, nonce, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	got, err := Open(key, ciphertext, nonce, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip = %q, want %q", got, plaintext)
	}
}

// TestCiphertextNeverContainsPlaintext is the most basic promise of the whole
// secret store: what gets written down must not be readable.
func TestCiphertextNeverContainsPlaintext(t *testing.T) {
	key, _ := NewKey()
	plaintext := []byte("super-secret-repository-password")

	ciphertext, _, err := Seal(key, plaintext, nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("the plaintext is present verbatim inside the ciphertext")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	key, _ := NewKey()
	other, _ := NewKey()

	ciphertext, nonce, err := Seal(key, []byte("secret"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := Open(other, ciphertext, nonce, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt for a wrong key, got %v", err)
	}
}

// TestOpenDetectsTampering covers the reason an AEAD is used at all: a
// modified ciphertext must fail loudly rather than decrypt to something else.
func TestOpenDetectsTampering(t *testing.T) {
	key, _ := NewKey()
	ciphertext, nonce, err := Seal(key, []byte("secret value"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, mutate := range []struct {
		name string
		fn   func()
	}{
		{"flip a ciphertext bit", func() { ciphertext[0] ^= 0x01 }},
		{"flip a nonce bit", func() { nonce[0] ^= 0x01 }},
		{"truncate the ciphertext", func() { ciphertext = ciphertext[:len(ciphertext)-1] }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			original := append([]byte(nil), ciphertext...)
			originalNonce := append([]byte(nil), nonce...)
			mutate.fn()

			if _, err := Open(key, ciphertext, nonce, nil); !errors.Is(err, ErrDecrypt) {
				t.Fatalf("expected ErrDecrypt after tampering, got %v", err)
			}

			ciphertext = original
			nonce = originalNonce
		})
	}
}

// TestAssociatedDataBindsCiphertextToItsPlace proves the protection against
// someone with database write access swapping one secret's ciphertext for
// another's. Without this binding the swap would decrypt cleanly and silently
// substitute the wrong credential.
func TestAssociatedDataBindsCiphertextToItsPlace(t *testing.T) {
	key, _ := NewKey()

	ciphertext, nonce, err := Seal(key, []byte("production password"), []byte("secret:repository:production"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Same key, same ciphertext, but presented as if it belonged to a
	// different secret.
	if _, err := Open(key, ciphertext, nonce, []byte("secret:repository:staging")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt when the associated data differs, got %v", err)
	}
}

// TestNoncesAreUnique guards against the single most damaging misuse of a
// stream cipher. XChaCha20-Poly1305's 24-byte nonce is what makes random
// generation safe here.
func TestNoncesAreUnique(t *testing.T) {
	key, _ := NewKey()
	seen := make(map[string]bool)

	for i := 0; i < 1000; i++ {
		_, nonce, err := Seal(key, []byte("same plaintext every time"), nil)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if seen[string(nonce)] {
			t.Fatal("a nonce was reused")
		}
		seen[string(nonce)] = true
	}
}

// TestSamePlaintextEncryptsDifferently confirms encryption is randomised, so
// an observer of the database cannot tell that two records hold equal values.
func TestSamePlaintextEncryptsDifferently(t *testing.T) {
	key, _ := NewKey()
	plaintext := []byte("identical")

	first, _, err := Seal(key, plaintext, nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, _, err := Seal(key, plaintext, nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("encrypting the same plaintext twice produced identical ciphertext")
	}
}

func TestDeriveKeyIsDeterministic(t *testing.T) {
	salt, _ := NewSalt()
	params := testParams()

	first, err := DeriveKey("master passphrase", salt, params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	second, err := DeriveKey("master passphrase", salt, params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("the same passphrase and salt must derive the same key")
	}
	if len(first) != KeySize {
		t.Fatalf("derived key is %d bytes, want %d", len(first), KeySize)
	}
}

func TestDeriveKeyVariesWithPassphraseAndSalt(t *testing.T) {
	saltA, _ := NewSalt()
	saltB, _ := NewSalt()
	params := testParams()

	base, _ := DeriveKey("passphrase", saltA, params)
	otherPassphrase, _ := DeriveKey("different passphrase", saltA, params)
	otherSalt, _ := DeriveKey("passphrase", saltB, params)

	if bytes.Equal(base, otherPassphrase) {
		t.Fatal("different passphrases must derive different keys")
	}
	if bytes.Equal(base, otherSalt) {
		t.Fatal("different salts must derive different keys")
	}
}

func TestDeriveKeyRejectsWeakParameters(t *testing.T) {
	salt, _ := NewSalt()

	for name, params := range map[string]KDFParams{
		"no time cost":      {TimeCost: 0, MemoryKiB: 64 * 1024, Parallelism: 1},
		"too little memory": {TimeCost: 1, MemoryKiB: 1024, Parallelism: 1},
		"no parallelism":    {TimeCost: 1, MemoryKiB: 64 * 1024, Parallelism: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DeriveKey("passphrase", salt, params); err == nil {
				t.Fatal("expected weak KDF parameters to be rejected")
			}
		})
	}
}

func TestDeriveKeyRejectsShortSalt(t *testing.T) {
	if _, err := DeriveKey("passphrase", []byte("short"), testParams()); err == nil {
		t.Fatal("expected a short salt to be rejected")
	}
}

func TestSealRejectsWrongKeySize(t *testing.T) {
	if _, _, err := Seal([]byte("too short"), []byte("data"), nil); err == nil {
		t.Fatal("expected a wrong-sized key to be rejected")
	}
}

func TestZero(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5}
	Zero(b)
	for i, v := range b {
		if v != 0 {
			t.Fatalf("byte %d was not zeroed: %d", i, v)
		}
	}
}
