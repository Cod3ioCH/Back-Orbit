# ADR-0005: Pure-Go SQLite driver (`modernc.org/sqlite`)

## Status
Accepted

## Context
Back-Orbit ships as a container image and should be straightforward to cross-compile and build in minimal (e.g. Alpine/musl) base images without a C toolchain.

## Decision
Use `modernc.org/sqlite`, a pure-Go SQLite implementation, instead of a CGO-based driver such as `mattn/go-sqlite3`.

## Consequences
- Static, CGO-free builds simplify the multi-stage Dockerfile and cross-compilation.
- Slightly different performance characteristics than the CGO driver; acceptable at the write volumes expected from a single-host backup scheduler and job queue.
- WAL mode and `busy_timeout` are configured explicitly at connection setup to handle concurrent readers/writers safely.
