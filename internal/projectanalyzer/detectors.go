package projectanalyzer

import (
	"fmt"
	"sort"
	"strings"
)

type databaseDetector struct{}

func (databaseDetector) ID() string { return "databases" }

type databaseSignature struct {
	technology, method, consistency string
	images, services, env           []string
}

var databaseSignatures = []databaseSignature{
	{"postgresql", "Create a logical dump with pg_dump before snapshotting persistent storage.", "application-consistent", []string{"postgres", "timescale"}, []string{"postgres", "database", "db"}, []string{"POSTGRES_DB", "POSTGRES_USER", "PGDATA"}},
	{"mariadb", "Create a logical dump with mariadb-dump before snapshotting persistent storage.", "application-consistent", []string{"mariadb"}, []string{"mariadb"}, []string{"MARIADB_DATABASE", "MARIADB_USER"}},
	{"mysql", "Create a logical dump with mysqldump before snapshotting persistent storage.", "application-consistent", []string{"mysql", "percona"}, []string{"mysql"}, []string{"MYSQL_DATABASE", "MYSQL_USER"}},
	{"mongodb", "Create a logical dump with mongodump; use replica-set aware options when available.", "application-consistent", []string{"mongo"}, []string{"mongo"}, []string{"MONGO_INITDB_DATABASE", "MONGO_INITDB_ROOT_USERNAME"}},
	{"valkey", "Persist data with a controlled SAVE/BGSAVE and capture the configured data directory.", "application-consistent", []string{"valkey"}, []string{"valkey"}, []string{}},
	{"redis", "Confirm whether Redis is durable or cache-only; for durable data run a controlled BGSAVE and capture RDB/AOF files.", "application-consistent", []string{"redis"}, []string{"redis", "cache"}, []string{}},
}

func (databaseDetector) Detect(input Input) []Finding {
	var findings []Finding
	for _, svc := range input.Services {
		for _, sig := range databaseSignatures {
			evidence := []Evidence{}
			if containsAny(strings.ToLower(svc.Image), sig.images) {
				evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name, Detail: "image matches " + sig.technology})
			}
			if containsAny(strings.ToLower(svc.Name), sig.services) {
				evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name, Detail: "service name suggests " + sig.technology})
			}
			for _, env := range svc.EnvironmentNames {
				if equalAny(env, sig.env) {
					evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name, Detail: "environment key " + env + " is present"})
				}
			}
			if len(evidence) == 0 {
				continue
			}
			confidence := ConfidencePossible
			if containsAny(strings.ToLower(svc.Image), sig.images) {
				confidence = ConfidenceConfirmed
			} else if len(evidence) > 1 {
				confidence = ConfidenceProbable
			}
			warnings := []string{}
			if sig.technology == "redis" || sig.technology == "valkey" {
				warnings = append(warnings, "Persistence and cache-only intent cannot be inferred safely; confirm before enabling backup.")
			}
			findings = append(findings, Finding{ID: "database:" + svc.Name + ":" + sig.technology, Kind: "database", Technology: sig.technology, Service: svc.Name, Confidence: confidence, Evidence: evidence, Recommendation: sig.method, Consistency: sig.consistency, Warnings: warnings})
		}
	}
	for _, path := range input.SQLite {
		source, detail := "filesystem", "SQLite file header detected"
		if strings.HasPrefix(path, "volume:") || strings.HasPrefix(path, "bind:") {
			source, detail = "mounted-storage", "SQLite file header detected in a read-only storage inspection"
		}
		findings = append(findings, Finding{ID: "database:sqlite:" + path, Kind: "database", Technology: "sqlite", Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: source, Subject: path, Detail: detail}}, Recommendation: "Use the SQLite online backup API or VACUUM INTO and include WAL/SHM handling before snapshotting.", Consistency: "application-consistent"})
	}
	return deduplicate(findings)
}

type storageDetector struct{}

func (storageDetector) ID() string { return "storage" }
func (storageDetector) Detect(input Input) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, svc := range input.Services {
		for _, m := range svc.Mounts {
			key := m.Type + ":" + m.Source + ":" + m.Target
			if seen[key] {
				continue
			}
			seen[key] = true
			tech := "bind-mount"
			rec := "Include this host path with explicit include/exclude rules and validate readability before each run."
			if m.Type == "volume" {
				tech = "named-volume"
				rec = "Capture this volume read-only through an inert helper container and preserve ownership and timestamps."
			}
			out = append(out, Finding{ID: "storage:" + key, Kind: "storage", Technology: tech, Service: svc.Name, Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: "compose", Subject: svc.Name, Detail: fmt.Sprintf("%s mounted at %s", m.Source, m.Target)}}, Recommendation: rec, Consistency: "filesystem-consistent"})
		}
	}
	return out
}

type metadataDetector struct{}

func (metadataDetector) ID() string { return "metadata" }
func (metadataDetector) Detect(input Input) []Finding {
	var out []Finding
	for _, name := range input.Secrets {
		out = append(out, Finding{ID: "secret:" + name, Kind: "secret", Technology: "compose-secret", Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: "compose", Subject: name, Detail: "secret reference declared; value was not read"}}, Recommendation: "Map this identifier to Back-Orbit's encrypted Secret Store during restore.", Consistency: "application-consistent"})
	}
	for _, name := range input.Configs {
		out = append(out, Finding{ID: "config:" + name, Kind: "configuration", Technology: "compose-config", Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: "compose", Subject: name, Detail: "configuration reference declared"}}, Recommendation: "Include the referenced configuration and preserve its relative restore path.", Consistency: "filesystem-consistent"})
	}
	for _, name := range input.EnvFiles {
		out = append(out, Finding{ID: "config:env-file:" + name, Kind: "configuration", Technology: "env-file", Confidence: ConfidenceConfirmed, Evidence: []Evidence{{Source: "compose", Subject: name, Detail: "environment file reference detected; contents were not read"}}, Recommendation: "Include this file encrypted and map secret values through stable Secret Store identifiers.", Consistency: "filesystem-consistent"})
	}
	return out
}

func DefaultDetectors() []ProjectDetector {
	return []ProjectDetector{databaseDetector{}, storageDetector{}, metadataDetector{}}
}
func containsAny(value string, candidates []string) bool {
	for _, c := range candidates {
		if strings.Contains(value, strings.ToLower(c)) {
			return true
		}
	}
	return false
}
func equalAny(value string, candidates []string) bool {
	for _, c := range candidates {
		if strings.EqualFold(value, c) {
			return true
		}
	}
	return false
}
func deduplicate(in []Finding) []Finding {
	seen := map[string]bool{}
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		if !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
