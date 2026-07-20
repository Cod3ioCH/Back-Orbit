# ADR-0010: Frontend build embedded into the Go binary

## Status
Accepted

## Decision
The production frontend build (`web/dist`) is embedded into the Go binary via `embed.FS` and served directly by the API server. The Vite dev server is used only for local development, proxying `/api` requests to the Go backend.

## Consequences
- Deployment is a single artifact/container — consistent with ADR-0001.
- Frontend and backend must be built together for a release; the Dockerfile's multi-stage build handles this (frontend build stage → Go build stage that embeds its output).
- No separate web server (nginx, etc.) is needed in the container.
