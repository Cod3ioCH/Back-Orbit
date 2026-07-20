// Package config loads Back-Orbit's runtime configuration from environment
// variables. There is no config file in the MVP; every setting has a safe,
// documented default suitable for local development.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the Back-Orbit server.
type Config struct {
	// HTTPAddr is the address the HTTP server binds to, e.g. "127.0.0.1:8080".
	// Defaults to localhost-only to avoid accidentally exposing an
	// unauthenticated-by-default instance on first run.
	HTTPAddr string

	// DataDir is the persistent data directory (SQLite database, staging
	// areas in later phases).
	DataDir string

	// DockerHost is the Docker daemon endpoint, e.g. "unix:///var/run/docker.sock".
	DockerHost string

	// SessionCookieName is the name of the session cookie.
	SessionCookieName string

	// SessionTTL is how long a session remains valid without activity.
	SessionTTL time.Duration

	// TrustProxyHeaders controls whether X-Forwarded-Proto is trusted to
	// mark cookies Secure. Only enable this behind a trusted reverse proxy.
	TrustProxyHeaders bool
}

// Load reads configuration from environment variables, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getEnv("BACKORBIT_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:           getEnv("BACKORBIT_DATA_DIR", "./data"),
		DockerHost:        getEnv("BACKORBIT_DOCKER_HOST", "unix:///var/run/docker.sock"),
		SessionCookieName: getEnv("BACKORBIT_SESSION_COOKIE", "backorbit_session"),
		TrustProxyHeaders: getEnvBool("BACKORBIT_TRUST_PROXY_HEADERS", false),
	}

	ttl, err := getEnvDuration("BACKORBIT_SESSION_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	cfg.SessionTTL = ttl

	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = dataDir

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	return cfg, nil
}

// DatabasePath returns the path to the SQLite database file.
func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "back-orbit.db")
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for %s: %w", key, err)
	}
	return d, nil
}
