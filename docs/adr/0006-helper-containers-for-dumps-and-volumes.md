# ADR-0006: Database dumps and named-volume staging via dedicated helper containers

## Status
Accepted (design fixed now; implementation lands in Phases 4–5)

## Context
Two approaches were considered for extracting data from running services: (a) `docker exec` into the existing target container and rely on tooling already present in its image, or (b) start a short-lived, Back-Orbit-owned helper container with known tooling, attached to the same Docker network/volume.

Option (a) is fragile: arbitrary third-party database images are not guaranteed to include `pg_dump`, `mysqldump`, or `mariadb-dump`, and giving Back-Orbit exec rights into every project container widens its footprint unpredictably.

## Decision
Both database dumps and named-volume staging run through ephemeral helper containers that Back-Orbit creates, controls, and removes:
- Database dump helpers carry known-good client tooling and connect to the target database over the network using credentials fetched just-in-time from the secret store, passed only via environment.
- Named-volume helpers mount the target volume read-only and stream its contents into a controlled staging area.

Helper containers are labeled for identification, removed via `defer` and OS signal handling on the happy path, and swept for orphans (by label) on Back-Orbit startup.

## Consequences
- Consistent, predictable tooling regardless of the target image's contents.
- Slightly higher overhead (spinning up a container per dump/volume) than an in-place `exec`, acceptable given backup jobs are not latency-sensitive.
- Requires careful container lifecycle management to avoid resource leaks — addressed by the defer/signal/orphan-sweep pattern above, and covered by tests when this phase is implemented.
