# Back-Orbit

Back-Orbit is a Docker-native backup and restore platform for Docker Compose projects. It runs as a single container, connects to the local Docker daemon, and is designed for homelab operators, self-hosters, and small teams running a single Docker host.

> **Status**: early-stage MVP. This repository currently implements the first vertical slice: project scaffolding, authentication, Docker Compose project discovery, and the audit/event backbone. See [`docs/architecture.md`](docs/architecture.md) for the full architecture and [`docs/adr/`](docs/adr) for the roadmap of what's still to come (backup engine, repositories, restore, retention, notifications, CLI).

## Why Back-Orbit

- **Docker-native**: discovers your running Compose projects via Docker labels, no separate inventory to maintain.
- **Security-first**: encrypted secret store (Argon2id + XChaCha20-Poly1305), session-based auth with CSRF protection, no shell string concatenation for backup tooling.
- **restic under the hood**: proven, encrypted, deduplicated backups, wrapped behind an internal engine interface.
- **Modern UI**: React + TypeScript + Tailwind + shadcn/ui, built for both technical and non-technical users.

## Repository layout

```
cmd/back-orbit/         Server binary entrypoint
cmd/back-orbit-cli/      CLI entrypoint (future phase)
internal/                Application code, organized by domain module
pkg/                      Shared DTOs usable by both server and CLI
web/                       React/TypeScript frontend (Vite)
migrations/                SQL migrations (goose)
deploy/                     Dockerfile and docker-compose examples
docs/                        Architecture, threat model, ADRs
```

See [`docs/architecture.md`](docs/architecture.md) for a full description of each module.

## Local development

### Prerequisites

- Go 1.22+
- Node.js 20+
- Docker (optional for the API/UI shell, required to see real Compose projects)

### Backend

```bash
go run ./cmd/back-orbit
```

Configuration is via environment variables — see [`.env.example`](deploy/.env.example). By default the server binds to `127.0.0.1:8080`, stores its SQLite database under `./data` (override with `BACKORBIT_DATA_DIR`), and talks to Docker at `unix:///var/run/docker.sock` (override with `BACKORBIT_DOCKER_HOST`).

### Frontend

```bash
cd web
npm install
npm run dev
```

The Vite dev server proxies `/api` to `http://127.0.0.1:8080`. In production, the built frontend is embedded into the Go binary and served directly — no separate web server is needed.

### Docker Compose (recommended for trying it out)

```bash
docker compose -f deploy/docker-compose.yml up --build
```

This mounts `/var/run/docker.sock` into the container (see the security note below) and persists data to a named volume. Open `http://localhost:8080` and complete the setup wizard.

Other deployment examples in [`deploy/`](deploy):

- [`docker-compose.socket-proxy.yml`](deploy/docker-compose.socket-proxy.yml) — recommended for anything beyond local trying-out: Back-Orbit talks to a [Docker Socket Proxy](https://github.com/Tecnativa/docker-socket-proxy) instead of the raw socket, so its own container needs no elevated privileges.
- [`docker-compose.secret.yml`](deploy/docker-compose.secret.yml) — an overlay showing the intended Docker Secret pattern for the master-key unlock (secret store ships in a later phase; not yet consumed by the running binary).
- [`docker-compose.minio.yml`](deploy/docker-compose.minio.yml) — a local S3-compatible [MinIO](https://min.io/) instance for developing/testing the S3 repository backend against, with no real cloud credentials needed.

## Security note: the Docker socket

Back-Orbit needs access to the Docker daemon to discover Compose projects and (in later phases) to run backup/restore helper containers. Mounting `/var/run/docker.sock` into a container is **equivalent to giving that container root access to the host**. Back-Orbit surfaces this explicitly in the UI and API (`GET /api/v1/docker/status`). For production deployments, prefer running behind a [Docker Socket Proxy](https://github.com/Tecnativa/docker-socket-proxy) with a minimal permission set, and always run Back-Orbit itself behind a TLS-terminating reverse proxy. See [`docs/threat-model.md`](docs/threat-model.md) for the full analysis.

## Tests

```bash
go build ./...
go vet ./...
go test ./...

cd web && npm test
```

## Documentation

- [Architecture overview](docs/architecture.md)
- [Threat model](docs/threat-model.md)
- [Architecture Decision Records](docs/adr)

## License

MIT — see [`LICENSE`](LICENSE). This is a placeholder chosen for a new open-source project; adjust before your first public release if you have different requirements.
