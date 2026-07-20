# ADR-0008: Versioned, additively-evolvable snapshot manifest schema

## Status
Accepted (design fixed now; implementation lands in Phase 4)

## Context
Every snapshot needs a machine-readable manifest describing everything required to restore it. This schema will need to grow over time (new fields, new database types, new consistency metadata) without breaking the ability to read manifests written by older versions.

## Decision
Every manifest carries an explicit `schemaVersion` field. Readers ignore unknown fields rather than rejecting them, and new fields are always added as optional. Breaking changes require a new major `schemaVersion` and an explicit migration path documented alongside the change.

## Consequences
- Old snapshots remain restorable after Back-Orbit upgrades.
- Manifest changes are additive by convention; reviewers should treat a non-additive manifest change as a red flag requiring a version bump and migration plan.
- The manifest schema will be published as a JSON Schema document once Phase 4 implements it, alongside the fields enumerated in `docs/architecture.md`.
