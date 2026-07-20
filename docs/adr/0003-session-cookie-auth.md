# ADR-0003: Session-cookie authentication instead of JWT

## Status
Accepted

## Context
Back-Orbit is a single-server application in the MVP. Authentication needs to support immediate, reliable revocation (logout, admin-forced logout, password change) and must not leak sensitive claims into a client-readable token.

## Decision
Use server-side sessions: an opaque random token is issued on login and set as an `HttpOnly`, `Secure` (when TLS-aware), `SameSite=Lax` cookie. Only a hash of the token is stored in the `sessions` table; the raw token is never persisted. Session validity, expiry, and revocation are enforced server-side on every request.

## Consequences
- Logout and expiry are immediate and centrally enforced — no waiting for a JWT to expire.
- Every authenticated request costs one indexed SQLite lookup; acceptable at MVP scale.
- CSRF protection is required (state-changing requests are otherwise vulnerable when auth rides on a cookie) — see ADR and the Threat Model.
- If Back-Orbit later needs to scale beyond a single server, session storage would need to move to a shared store; not a concern for the current single-container architecture (ADR-0001).
