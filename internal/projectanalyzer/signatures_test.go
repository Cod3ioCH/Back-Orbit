package projectanalyzer

import (
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/imageref"
)

// realEngineImages are published images that really are the engine named.
// They are the fixture for both directions of the signature contract: every
// one of them must be recognised, and every pattern must recognise one of
// them.
var realEngineImages = map[string]string{
	"postgres:17-alpine":                          "postgresql",
	"ghcr.io/immich-app/postgres:14-vectorchord0": "postgresql",
	"timescale/timescaledb:latest-pg17":           "postgresql",
	"timescale/timescaledb-ha:pg17":               "postgresql",
	"mariadb:11":                                  "mariadb",
	"mysql:8":                                     "mysql",
	"mysql/mysql-server:8.0":                      "mysql",
	"percona:8.0":                                 "mysql",
	"percona/percona-server:8.0":                  "mysql",
	"percona/percona-xtradb-cluster:8.0":          "mysql",
	"mongo:7":                                     "mongodb",
	"mongodb/mongodb-community-server:8.0-ubi8":   "mongodb",
	"valkey/valkey:8-bookworm":                    "valkey",
	"redis:7-alpine":                              "redis",
}

// TestEveryEngineImageIsRecognised. Matching whole repository segments is
// stricter than the substring search it replaced, and strictness cuts both
// ways: "timescale" used to catch TimescaleDB by accident, and as a repository
// path it is only the namespace, matching nothing at all.
func TestEveryEngineImageIsRecognised(t *testing.T) {
	for image, technology := range realEngineImages {
		t.Run(image, func(t *testing.T) {
			finding := only(t, detect(service("db", image)))
			if finding.Technology != technology {
				t.Errorf("technology = %q, want %q", finding.Technology, technology)
			}
		})
	}
}

// TestEverySignaturePatternMatchesARealImage. A pattern that matches nothing
// is an engine Back-Orbit can never detect, and nothing in the code would ever
// say so — the same guard the template catalog carries.
func TestEverySignaturePatternMatchesARealImage(t *testing.T) {
	paths := make([]string, 0, len(realEngineImages))
	for image := range realEngineImages {
		paths = append(paths, imageref.RepositoryPath(image))
	}

	for _, sig := range databaseSignatures {
		for _, pattern := range sig.imagePatterns {
			matched := false
			for _, path := range paths {
				if imageref.Matches(path, pattern) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("%s: pattern %q matches none of the real images it is meant to recognise",
					sig.technology, pattern)
			}
		}
	}
}

// TestToolsNamedAfterAnEngineAreNotDataStores.
//
// These all carry an engine's name inside their image reference, and a
// substring search reported every one of them under "Detected data stores": an
// admin UI, three metrics exporters and a monitoring server, listed as the
// databases they merely look at. None of them holds the data, so a plan built
// on them protects nothing — and the operator reading that list has no way to
// tell the invented entries from the real ones.
func TestToolsNamedAfterAnEngineAreNotDataStores(t *testing.T) {
	for _, image := range []string{
		"mongo-express:1.0.2",
		"prometheuscommunity/postgres-exporter:v0.15",
		"prom/mysqld-exporter:v0.15",
		"oliver006/redis_exporter:v1.62",
		"percona/pmm-server:2",
	} {
		t.Run(image, func(t *testing.T) {
			if findings := detect(service("tool", image)); len(findings) != 0 {
				t.Fatalf("got %+v, want no database finding", findings)
			}
		})
	}
}
