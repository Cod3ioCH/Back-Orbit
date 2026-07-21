# syntax=docker/dockerfile:1

# ---- Frontend build stage ----------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Backend build stage ------------------------------------------------
FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overwrite the placeholder dist/ with the real frontend build before
# go:embed picks it up (see web/embed.go and docs/adr/0010-embedded-frontend-build.md).
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/back-orbit ./cmd/back-orbit

# ---- Runtime stage --------------------------------------------------------
FROM alpine:3.22 AS runtime

# restic is the backup engine (docs/adr/0002-restic-backup-engine.md), so the
# image is useless without it. The version is pinned and the download verified
# against the checksums published with that release: the binary that actually
# touches every backup is exactly the supply-chain risk docs/threat-model.md
# calls out, so an unpinned "latest" download would undermine the whole point.
ARG TARGETARCH
ARG RESTIC_VERSION=0.19.1
ARG RESTIC_SHA256_AMD64=f415415624dcc452f2a02b8c33641791a8c6d6d3b65bbb3543fcf9a25151585c
ARG RESTIC_SHA256_ARM64=a5f64aaab53d51e311fa3829124c5b703f2d14cf187d8640b6be3b2b49376465

RUN apk add --no-cache ca-certificates wget bzip2 && \
    arch="${TARGETARCH:-$(uname -m)}" && \
    case "$arch" in \
      amd64|x86_64)  arch=amd64; sha="$RESTIC_SHA256_AMD64" ;; \
      arm64|aarch64) arch=arm64; sha="$RESTIC_SHA256_ARM64" ;; \
      *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac && \
    wget -q -O /tmp/restic.bz2 \
      "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_${arch}.bz2" && \
    echo "${sha}  /tmp/restic.bz2" | sha256sum -c - && \
    bunzip2 /tmp/restic.bz2 && \
    install -m 0755 /tmp/restic /usr/local/bin/restic && \
    rm -f /tmp/restic /tmp/restic.bz2 && \
    apk del bzip2 && \
    restic version && \
    addgroup -g 10001 backorbit && \
    adduser -D -u 10001 -G backorbit backorbit && \
    mkdir -p /var/lib/back-orbit /etc/back-orbit /backups && \
    chown -R backorbit:backorbit /var/lib/back-orbit /etc/back-orbit /backups

COPY --from=backend /out/back-orbit /usr/local/bin/back-orbit

ENV BACKORBIT_HTTP_ADDR=0.0.0.0:8080 \
    BACKORBIT_DATA_DIR=/var/lib/back-orbit \
    BACKORBIT_DOCKER_HOST=unix:///var/run/docker.sock

VOLUME ["/var/lib/back-orbit"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

# Runs as a non-root user by default. Direct Docker socket access still
# requires this user to be able to read/write /var/run/docker.sock — see
# deploy/docker-compose.yml and README.md for the supported ways to grant
# that (group_add with the host's docker group GID, or preferably a Docker
# Socket Proxy, which needs no special container privileges at all).
USER backorbit:backorbit

ENTRYPOINT ["/usr/local/bin/back-orbit"]
