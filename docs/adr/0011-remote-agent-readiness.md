# ADR-0011: Remote-agent extensibility prepared, not implemented

## Status
Accepted

## Context
The MVP targets a single Docker host, but the product direction anticipates managing multiple remote hosts from one Back-Orbit instance in the future. Retrofitting that later without any forethought risks a disruptive rewrite of the Docker and project domain layers.

## Decision
Define Docker access behind a `docker.Client` Go interface (implemented today only by a local-socket client), and add a `host_identity` field to the `Project` domain model now, even though it always refers to the local host in the MVP. No remote transport (SSH tunnel, remote agent protocol, etc.) is implemented in this phase.

## Consequences
- Future remote-host support is additive: a new `docker.Client` implementation plus multi-host project registration, without changing callers that only depend on the interface.
- No remote-agent code exists yet, so there is nothing to secure or test in this phase beyond keeping the interface boundary clean.
