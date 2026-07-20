# ADR-0001: Single-container architecture (Go + SQLite + embedded SPA)

## Status
Accepted

## Context
Back-Orbit targets homelab operators and small teams running a single Docker host. Operational simplicity — one image, one volume, no external database or message broker to provision — is more valuable to this audience than horizontal scalability.

## Decision
Ship Back-Orbit as a single Go binary/container that serves the REST API, Server-Sent Events, and the built React frontend (embedded via `embed.FS`), backed by a single SQLite database file on a persistent volume.

## Consequences
- Trivial deployment (`docker run` / `docker compose up`) with no external dependencies.
- SQLite's single-writer model is sufficient at the expected job concurrency of a single host; WAL mode plus per-resource locking (see ADR-0007) keeps this from becoming a bottleneck.
- Horizontal scaling and multi-host coordination are out of scope for the MVP; the module boundaries (see `docs/architecture.md`) are kept clean enough that this could change later without a full rewrite.
