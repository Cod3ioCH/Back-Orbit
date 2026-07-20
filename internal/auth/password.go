package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2Params are the Argon2id parameters used for password hashing.
// These follow OWASP's current baseline recommendation for interactive
// login (as opposed to the higher-cost parameters used for the secret-store
// master key derivation, which trades latency for stronger protection since
// it runs far less frequently).
type argon2Params struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultArgon2Params = argon2Params{
	memoryKiB:   64 * 1024, // 64 MiB
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

// HashPassword hashes a plaintext password with Argon2id, returning an
// encoded string that embeds the parameters and salt so it can be verified
// later even if defaultArgon2Params changes.
func HashPassword(password string) (string, error) {
	salt := make([]byte, defaultArgon2Params.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		defaultArgon2Params.iterations,
		defaultArgon2Params.memoryKiB,
		defaultArgon2Params.parallelism,
		defaultArgon2Params.keyLength,
	)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		defaultArgon2Params.memoryKiB,
		defaultArgon2Params.iterations,
		defaultArgon2Params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	return encoded, nil
}

// VerifyPassword checks a plaintext password against a hash produced by
// HashPassword, using a constant-time comparison to avoid timing attacks.
func VerifyPassword(encodedHash, password string) (bool, error) {
	params, salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	candidate := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memoryKiB,
		params.parallelism,
		uint32(len(hash)),
	)

	return subtle.ConstantTimeCompare(hash, candidate) == 1, nil
}

func decodeHash(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid password hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid password hash version: %w", err)
	}
	if version != argon2.Version {
		return argon2Params{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var params argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memoryKiB, &params.iterations, &params.parallelism); err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid password hash params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid password hash salt: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("invalid password hash digest: %w", err)
	}

	return params, salt, hash, nil
}
