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

Image patterns are compared against the *repository path* — registry host, tag
and digest removed — on whole path segments, anchored at the end. Substring
matching was wrong in both directions: `mongo` claimed the `mongo-express`
container, which left the database role unfilled and the template unmatched,
and any product name appearing in a registry host would have matched
everything published under it.

Required roles are filled as an assignment, not first-come-first-served: each
role takes a distinct image, and a role that could be filled by two images does
not strand a later role by taking the wrong one. The order containers come back
from Docker is not something the matcher gets to choose, so it must not change
the answer.

The displayed score measures how much of the topology a template describes was
actually found. Required evidence is a precondition rather than a variable —
every eligible match carries all of it — so what the number varies with is the
optional components. A project that has everything the template describes
scores 100. Alternative implementations of one component ("redis or valkey")
are declared as a group for the same reason required roles are: a complete
project must not be reported as a half match.

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
