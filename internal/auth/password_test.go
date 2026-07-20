package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword(hash, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password to fail verification")
	}
}

func TestHashPasswordProducesUniqueSalts(t *testing.T) {
	hash1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	hash2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if hash1 == hash2 {
		t.Fatal("expected two hashes of the same password to differ due to random salts")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	_, err := VerifyPassword("not-a-valid-hash", "anything")
	if err == nil {
		t.Fatal("expected an error for a malformed hash")
	}
}

func TestHashPasswordNeverContainsPlaintext(t *testing.T) {
	const password = "super-secret-plaintext-marker"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, password) {
		t.Fatal("hash must never contain the plaintext password")
	}
}
