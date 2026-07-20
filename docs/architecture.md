# Architecture Overview

## Product summary

Back-Orbit is a self-hosted, Docker-native backup and restore platform for Docker Compose projects on a single Docker host, architected so a remote-agent transport can be added later without breaking the domain model. The application runs as a single container with access to `/var/run/docker.sock`. It backs up Compose files, `.env`/`env_file` references, configuration files and directories, Docker named volumes, bind mounts, consistent database dumps, and encrypted project secrets, and it produces a machine-readable restore manifest per snapshot.

The backup engine is [restic](https://restic.net/), wrapped behind an internal `BackupEngine` interface so it could be swapped later. Database dumps (PostgreSQL, MySQL, MariaDB) and named-volume staging both run through short-lived, Back-Orbit-owned helper containers rather than `docker exec` into arbitrary target containers, because target images aren't guaranteed to contain dump tooling. Secrets are encrypted at rest with Argon2id key derivation and XChaCha20-Poly1305 AEAD, in a two-layer scheme (master password/key unlocks a Data Encryption Key, which in turn protects individual secrets).

## Layers

- **API layer** (`internal/api`): HTTP router (chi), a middleware chain (panic recovery, structured + redacted logging, security headers, CSRF, session auth, rate limiting), request validation, and Server-Sent Events endpoints for live updates.
- **Domain/service layer**: one package per concern (`auth`, `projects`, `backup`, `restore`, `repositories`, `secrets`, `jobs`, `scheduler`, `retention`, `notifications`, `verification`) containing business logic independent of the HTTP transport.
- **Infrastructure adapters**:
  - `internal/docker` — Docker client wrapper (`docker/docker/client` SDK) against a configurable Docker host (`unix:///var/run/docker.sock` by default).
  - `internal/backup` — restic subprocess wrapper using `exec.CommandContext` exclusively, never shell string concatenation.
  - `internal/database` — SQLite access (`database/sql`, WAL mode) with goose migrations embedded via `embed.FS`.
- **Persistence**: the repository pattern is used selectively — where it earns its keep (projects, plans, jobs, snapshots, secrets) — not as a blanket abstraction over simple CRUD.
- **Scheduler**: `robfig/cron/v3` drives scheduled job creation (added in the backup-plans phase).
- **Job runner**: a SQLite-table-backed queue with an in-process worker pool; no external broker is needed at single-node MVP scale. Locking is per `(plan_id)` and per `(repository_id)` to prevent overlapping runs.
- **Event bus**: an in-process publish/subscribe broker feeds Server-Sent Event streams, persists to the audit log, and (later) triggers notification providers.
- **Secrets**: `internal/crypto` wraps `golang.org/x/crypto/argon2` (Argon2id) and `golang.org/x/crypto/chacha20poly1305` (`NewX`, XChaCha20-Poly1305) — no custom cryptography.
- **Docker security posture**: `GET /api/v1/docker/status` reports connectivity and an explicit warning that socket access is root-equivalent; the UI surfaces this as a persistent banner.
- **Remote-agent readiness**: the `docker.Client` interface and a `host_identity` field on `Project` are the seams a future remote-transport implementation would use; no remote transport exists yet.

## Component and data-flow diagram

```
[Browser SPA] --HTTPS (REST + SSE)--> [Go API Server]
   [Go API Server] --unix socket--> [Docker Daemon] --spawns--> [Helper Container] (volume staging, DB dump)
   [Go API Server] --exec.CommandContext--> [restic subprocess] --network/fs--> [Repository: local | SFTP | S3]
   [Go API Server] <--database/sql--> [SQLite: /var/lib/back-orbit/back-orbit.db (WAL)]
   [Scheduler (cron)] --creates--> [Job Queue (SQLite table)] --consumed by--> [Job Runner worker pool]
   [Job Runner] --emits--> [Event Bus] --> [SSE per job/activity] + [audit_events table] + [Notification providers]
   [Secret Store] --unlocked by--> [Master password | Docker secret/key file] --protects DEK--> [encrypted secrets in SQLite]
```

Example backup job data flow: scheduler/manual trigger → job `queued` → lock check → `preparing` (resolve Compose/volumes/secrets) → `dumping_database` (helper container per DB target) → `snapshotting` (restic backup of staging directories + config files) → `uploading` (restic's own transport to the repository) → `verifying` (optional) → `applying_retention` → `completed` / `completed_with_warnings` / `failed`, emitting events and audit entries at every phase transition.

## Job and event model

Job status values: `queued → preparing → dumping_database → snapshotting → uploading → verifying → applying_retention → completed | completed_with_warnings | failed | cancelled`. Every phase transition emits a `job.phase_changed` event; progress emits `job.progress`; log lines emit `job.log` (redaction-checked before persistence or transmission); terminal states emit `job.completed` / `job.failed` / `job.cancelled`. Events flow through the in-process broker to (a) per-job SSE subscribers and a global activity stream, (b) `audit_events`/`job_events` persistence, and (c) notification providers (later phase). The vertical slice implemented in this repository establishes the generic event/audit backbone (auth events, project-scan events); the full job state machine ships with the backup-plans phase.

## Repository structure

```
cmd/
  back-orbit/            Server binary
  back-orbit-cli/         CLI (future phase)
internal/
  api/                    Router, handlers, middleware
  auth/                   Argon2id, sessions, CSRF, rate limiting
  backup/                 restic wrapper (future phase)
  config/                 Environment-based configuration
  crypto/                 Argon2id / XChaCha20-Poly1305 primitives (future phase)
  database/                SQLite connection + goose migrations
  docker/                  Docker client wrapper, Compose discovery
  events/                  Pub/sub + audit persistence
  jobs/                    Job engine (future phase)
  notifications/           Notification providers (future phase)
  projects/                Project service
  repositories/             Backup repository management (future phase)
  restore/                 Restore engine (future phase)
  retention/                Retention policies (future phase)
  scheduler/                Cron scheduling (future phase)
  secrets/                  Secret store (future phase)
  storage/                   Named-volume/bind-mount staging (future phase)
  verification/              Repository/snapshot verification (future phase)
pkg/                         Shared DTOs (server + CLI)
web/                          React/TypeScript frontend
migrations/                    SQL migration files (goose)
deploy/                         Dockerfile, docker-compose examples
docs/                             This documentation
```

## Implementation phases

1. **Phase 1 (this repository)** — backend/frontend scaffold, SQLite migrations, admin setup, login/session, Docker connection + Compose project discovery, project UI, event/audit model, dev Compose environment, tests.
2. Repositories (local/SFTP/S3) + secret store + restic engine wrapper.
3. Backup plans + scheduler + job engine + SSE.
4. Volume/bind-mount backup (helper containers) + snapshot manifest.
5. Database adapters (PostgreSQL/MySQL/MariaDB).
6. Restore engine + wizard + dry run.
7. Retention + health score + monitoring/alerts/notifications.
8. CLI.
9. Hardening, full documentation, disaster-recovery test guide, remote-agent polish.

See `docs/adr/` for the architecture decisions behind these choices.
