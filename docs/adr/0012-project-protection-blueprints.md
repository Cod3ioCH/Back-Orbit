# ADR-0012: Evidence-based project protection blueprints

## Status

Accepted.

## Context

Image names and service names are useful signals, but they do not prove how an application persists data. Automatically treating every Redis container as durable, or every mounted directory as safe to overwrite, would create false confidence and unsafe restores. The analyzer must also avoid collecting credential values while inspecting Compose configuration.

## Decision

Back-Orbit analyzes Compose files and live Docker metadata through detector plugins. Detectors emit findings with explicit evidence and one of three confidence levels: `confirmed`, `probable`, or `possible`. A separate planner turns those findings into a versioned protection blueprint and recommended backup sequence.

Only environment variable names and Compose secret identifiers may enter analyzer output. Values are neither retained nor returned by the API. File discovery is bounded, does not follow symlinks, and is restricted to the registered project directory. A blueprint can be explicitly confirmed; subsequent fingerprint changes are reported as drift.

Analysis is advisory. It does not execute database commands, stop containers, or mutate backup plans.

## Consequences

- Operators can understand why a component was detected and correct uncertain conclusions.
- Backup plan generation can later consume a stable, versioned blueprint without coupling detectors to job execution.
- New technologies can be added as detector plugins.
- Detection remains incomplete when Compose files are unavailable or applications hide persistence behind custom images; the UI must state this clearly.
