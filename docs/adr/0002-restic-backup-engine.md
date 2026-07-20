# ADR-0002: restic as the sole backup engine, behind a `BackupEngine` interface

## Status
Accepted

## Context
Backup correctness and restore reliability are the product's top priority. Writing a custom deduplicating, encrypting backup format is a large, security-critical undertaking that a mature open-source tool already solves well.

## Decision
Use [restic](https://restic.net/) as the only backup engine in the MVP, invoked exclusively through structured, argument-array subprocess calls (`exec.CommandContext`, never shell string concatenation). All restic interaction is mediated by an internal `BackupEngine` Go interface (`InitRepository`, `CreateSnapshot`, `ListSnapshots`, `RestoreSnapshot`, `VerifyRepository`, `ApplyRetention`, `PruneRepository`) so a different engine could be substituted later without changing callers.

## Consequences
- We inherit restic's encryption, deduplication, and repository backends (local, SFTP, S3-compatible) instead of re-implementing them.
- restic's password handling (never as a CLI argument; via `RESTIC_PASSWORD_COMMAND` or a temp file/stdin) must be respected end-to-end in the wrapper.
- The wrapper is the single place that needs rigorous testing for command construction, timeout/cancellation handling, and structured stdout/stderr capture — see the Threat Model's "command injection" mitigation.
- This interface is implemented in a later phase (Phase 2); this ADR fixes the shape now so downstream job-engine code (Phase 3) can be written against it.
