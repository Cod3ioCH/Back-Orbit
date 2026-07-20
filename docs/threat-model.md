# Threat Model

This is a STRIDE-oriented threat model for Back-Orbit. It will be extended as later phases (secrets, repositories, restore) land; the mitigations noted here are architectural commitments, not aspirational.

## Assets

- restic repository passwords
- Database credentials
- Backup contents (potentially containing PII)
- Master password / Data Encryption Key (DEK)
- Session cookies
- Docker socket access (root-equivalent on the host)

## Trust boundaries

- Browser ↔ API
- API ↔ Docker socket (the highest-privilege boundary in the system)
- API ↔ external repositories (SFTP/S3 credentials)
- API ↔ restic subprocess
- API ↔ helper containers

## Threats and mitigations

| Threat | Mitigation |
|---|---|
| Docker socket privilege escalation | Persistent warning banner in UI/API (`GET /api/v1/docker/status`), documentation for running behind a Docker Socket Proxy, minimal container capabilities, socket never proxied to the frontend |
| Secret exfiltration via logs | Redaction middleware; an allow-list (not a deny-list) governs which fields are logged |
| Path traversal (file browser, restore) | Canonicalize paths and prefix-check against allowed roots; reject symlink escapes |
| Command injection (restic / dump tools) | Exclusively `exec.CommandContext` with argument arrays, never a shell; an allow-list of flags; strict validation of user-supplied identifiers |
| SSRF via repository URL (S3/SFTP) | Scheme/host validation; rejection of cloud metadata IPs (e.g. `169.254.169.254`); optional host allow-list |
| Session hijacking | HttpOnly + Secure + SameSite cookies, short session lifetime, rotation on login, CSRF token required for state-changing requests |
| Brute-force login | Rate limiting and backoff per IP + username, audit logging of failed attempts |
| Symlink / zip-slip during restore | Symlinks pointing outside the restore target root are rejected by default |
| Database credential exposure via dumps | Credentials are fetched just-in-time from the secret store, passed only via environment to helper containers, never written to logs or disk in plaintext; staging directories use `0600`/`0700` permissions and are securely removed immediately after use |
| Supply-chain risk (restic binary, Go dependencies) | Pinned restic version with checksum verification in the Dockerfile; `go.sum` verification in CI |
| Denial of service via unbounded logs/jobs | Bounded per-job log ring buffer, log rotation |

## Current implementation status (Phase 1)

This repository currently implements authentication, session management, and Docker Compose project discovery. Relevant mitigations already in place:

- Passwords hashed with Argon2id (`internal/auth`), never stored or logged in plaintext.
- Sessions are opaque random tokens; only a SHA-256 hash of the token is stored in SQLite, so a database read alone does not yield valid session tokens.
- Cookies are `HttpOnly`, `SameSite=Lax`, and `Secure` when the server is configured for TLS or sits behind a proxy that sets `X-Forwarded-Proto: https`.
- CSRF protection via a double-submit token required on all state-changing requests.
- Login attempts are rate-limited per (IP, username) pair with exponential backoff.
- All authentication and project actions are recorded in `audit_events`.
- Structured logging redacts known-sensitive field names before they reach the log sink.
- The Docker client only ever reads container/image/volume/network metadata in this phase; it does not yet spawn helper containers (that lands with the volume/database backup phases, at which point this document will be extended with the corresponding mitigations already described in "Threats and mitigations" above).

## Accepted risk: lost master password

Once the secret store ships (Phase 2), losing the master password/unlock key is an **unrecoverable, by-design** state: Back-Orbit does not retain any means to recover a forgotten master password, because doing so would itself be a backdoor. This will be documented prominently in the setup wizard and operations guide, with a recommendation to back up critical repository passwords offsite through a separate channel.

## Not yet covered by this document

SSRF handling for repository endpoints, the full secret-store threat surface, and restore-time destructive-action confirmation flows will be detailed here as their respective implementation phases land.
