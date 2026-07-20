# ADR-0009: Repository pattern used selectively, not globally

## Status
Accepted

## Context
The Go repository pattern (a data-access interface per entity) adds indirection that pays off when there are multiple implementations, when persistence logic is complex, or when a domain boundary genuinely benefits from being decoupled from SQL. Applied uniformly to every table, it becomes ceremony without benefit.

## Decision
Use an explicit repository interface only where it has a clear payoff: projects, backup plans, jobs, snapshots, and secrets — entities with non-trivial query patterns, multiple call sites, or a testing need to substitute a fake. Simpler, single-purpose lookups (e.g. session validation) use `database/sql` directly inside the owning service.

## Consequences
- Less boilerplate for simple CRUD paths.
- Reviewers should push back on new repository interfaces that wrap trivial single-query access — that's a sign the abstraction isn't earning its keep.
