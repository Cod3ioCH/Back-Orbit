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
RUN apk add --no-cache ca-certificates wget && \
    addgroup -g 10001 backorbit && \
    adduser -D -u 10001 -G backorbit backorbit && \
    mkdir -p /var/lib/back-orbit /etc/back-orbit && \
    chown -R backorbit:backorbit /var/lib/back-orbit /etc/back-orbit

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
