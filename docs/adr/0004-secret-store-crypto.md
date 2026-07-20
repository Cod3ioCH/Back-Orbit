# ADR-0004: Secret store cryptography — Argon2id + XChaCha20-Poly1305, two-layer key hierarchy

## Status
Accepted (design fixed now; implementation lands in Phase 2)

## Context
Back-Orbit stores repository credentials, database credentials, notification credentials, and project secrets. These must never be recoverable from the SQLite file alone, must support key rotation, and must use only established, audited cryptographic primitives — no custom cryptography.

## Decision
- Derive a key-encryption key from the operator's master password using **Argon2id** with a random per-installation salt.
- Use that key to protect a randomly generated **Data Encryption Key (DEK)**, stored encrypted, never in plaintext.
- Encrypt individual secrets with the DEK using **XChaCha20-Poly1305** (AEAD), with a unique random nonce per encryption.
- Store a key version alongside each encrypted secret to support future key rotation without a big-bang re-encryption.
- Never accept the master password via a regular environment variable as the recommended path; support (a) interactive unlock after restart, and (b) unattended unlock via a Docker secret or a permission-restricted key file.

## Consequences
- If the master password/unlock key is lost, encrypted secrets are unrecoverable by design (documented in the Threat Model) — this is a deliberate trade-off, not an oversight.
- All cryptographic primitives come from `golang.org/x/crypto` (a maintained, audited Go package), not a bespoke implementation.
- API responses, logs, and audit events must never include decrypted secret values — enforced by construction (decrypted values only exist transiently in memory at the point of use, e.g. passing a DB password to a helper container).
