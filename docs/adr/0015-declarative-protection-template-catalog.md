# ADR-0015: Declarative protection template catalog

## Status

Accepted.

## Context

The project analyzer records evidence about one concrete Compose project, but
evidence alone does not encode application-specific recovery knowledge. Common
applications such as Nextcloud, Vaultwarden, and Gitea have coordinated data,
database, secret, and verification requirements. Treating an image-name match
as permission to execute hooks or activate a backup plan would create unsafe
automation and false confidence.

## Decision

Back-Orbit embeds a versioned catalog of declarative `ProtectionTemplate`
documents. Matching consumes only normalized image references and already
redacted analyzer findings. Every required service role and required technology
must match; optional evidence only improves the displayed score. Match results
include the evidence, missing optional components, template version, advisory
plan, and restore checks.

Templates contain no executable commands and cannot activate a job. The actual
Compose project remains the source of truth, and an operator must review and
confirm the generated project blueprint. Template identity and version are part
of the blueprint fingerprint so catalog changes can trigger a new review.

The built-in catalog is compiled into the binary and rejected on unknown YAML
fields, duplicate IDs, unsupported schema versions, or invalid identifiers.
Locally installed and community templates are deferred until signature, trust,
upgrade, and executable-hook policies are designed.

The test lab separates versioned templates from generated runtime projects.
Generated credentials and application state remain under `lab/runtime/`, which
is excluded from Git.

## Alternatives considered

- Hard-code application rules in detectors: simple initially, but couples
  evidence collection to policy and makes template versioning opaque.
- Execute upstream backup scripts directly: feature-rich, but creates a remote
  code execution and supply-chain boundary unsuitable for implicit matching.
- Back up every filesystem path: easy to explain, but captures caches and build
  artifacts while still missing logical consistency and secret dependencies.

## Consequences

- Application recommendations are explainable, testable, and versioned.
- False-positive matches are biased toward no match instead of unsafe advice.
- The initial catalog can recommend but not automatically enforce hooks.
- Template maintenance and compatibility testing become an ongoing product
  responsibility.
