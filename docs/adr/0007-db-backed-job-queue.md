# ADR-0007: SQLite-backed job queue with an in-process worker pool

## Status
Accepted (design fixed now; implementation lands in Phase 3)

## Context
Backup jobs need a persistent queue (surviving process restarts), cancellation, timeouts, retry, and per-plan/per-repository locking to prevent overlapping runs. Introducing an external message broker (Redis, RabbitMQ, etc.) would contradict the single-container deployment goal (ADR-0001).

## Decision
Model the job queue as a table in the existing SQLite database, consumed by an in-process Go worker pool. Locking is implemented as advisory-lock rows keyed by `plan_id` and by `repository_id`, checked transactionally before a job transitions out of `queued`.

## Consequences
- No new infrastructure dependency.
- Job state survives a process restart; a startup routine detects jobs left in a non-terminal state from a previous run and marks them appropriately (not silently resumed mid-phase).
- This design does not scale beyond a single Back-Orbit process. If multi-host/multi-instance operation is ever required, this queue would need to move to a coordinated store — out of scope for the MVP and consistent with ADR-0001's single-host focus.
