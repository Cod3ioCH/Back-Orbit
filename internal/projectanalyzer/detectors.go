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
	images, env                     []string
	// dataDirs are the paths the engine keeps its files in. A service mounting
	// one of these is storing that engine's data — which is the thing a backup
	// actually cares about, and far stronger evidence than a name.
	dataDirs []string
	// genericDataDir marks a data directory too common to mean anything on its
	// own. "/data" is used by Redis, Valkey and half the images on Docker Hub;
	// "/var/lib/postgresql/data" is used by PostgreSQL.
	genericDataDir bool
}

var databaseSignatures = []databaseSignature{
	{technology: "postgresql", method: "Create a logical dump with pg_dump before snapshotting persistent storage.", consistency: "application-consistent",
		images: []string{"postgres", "timescale"}, env: []string{"POSTGRES_DB", "POSTGRES_USER", "PGDATA"},
		dataDirs: []string{"/var/lib/postgresql/data", "/var/lib/postgresql"}},
	{technology: "mariadb", method: "Create a logical dump with mariadb-dump before snapshotting persistent storage.", consistency: "application-consistent",
		images: []string{"mariadb"}, env: []string{"MARIADB_DATABASE", "MARIADB_USER"},
		dataDirs: []string{"/var/lib/mysql"}},
	{technology: "mysql", method: "Create a logical dump with mysqldump before snapshotting persistent storage.", consistency: "application-consistent",
		images: []string{"mysql", "percona"}, env: []string{"MYSQL_DATABASE", "MYSQL_USER"},
		dataDirs: []string{"/var/lib/mysql"}},
	{technology: "mongodb", method: "Create a logical dump with mongodump; use replica-set aware options when available.", consistency: "application-consistent",
		images: []string{"mongo"}, env: []string{"MONGO_INITDB_DATABASE", "MONGO_INITDB_ROOT_USERNAME"},
		dataDirs: []string{"/data/db"}},
	{technology: "valkey", method: "Persist data with a controlled SAVE/BGSAVE and capture the configured data directory.", consistency: "application-consistent",
		images: []string{"valkey"}, dataDirs: []string{"/data"}, genericDataDir: true},
	{technology: "redis", method: "Confirm whether Redis is durable or cache-only; for durable data run a controlled BGSAVE and capture RDB/AOF files.", consistency: "application-consistent",
		images: []string{"redis"}, dataDirs: []string{"/data"}, genericDataDir: true},
}

// dataMountFor returns the mount holding this engine's files, if the service
// declares one.
func dataMountFor(svc ServiceEvidence, sig databaseSignature) *MountEvidence {
	for i, mount := range svc.Mounts {
		target := strings.TrimSuffix(mount.Target, "/")
		for _, dir := range sig.dataDirs {
			if target == strings.TrimSuffix(dir, "/") {
				return &svc.Mounts[i]
			}
		}
	}
	return nil
}

// detectDatabase decides whether a service stores this engine's data, and how
// sure that is.
//
// Confidence is earned from the *kind* of evidence, never from how many weak
// hints happen to stack up. The rules exist because the previous ones produced
// confident nonsense: a Prometheus postgres-exporter was reported as a
// confirmed PostgreSQL database to be dumped, a MySQL service named "db" also
// became a possible PostgreSQL, and a memcached named "cache" became Redis.
// Each of those turns a backup plan into a guess, and a wrong "confirmed" is
// worse than an honest gap.
//
// A service name is deliberately not evidence of a technology at all. "db"
// says a database is likely; it does not say which engine, and inventing one
// is a guess dressed up as a finding.
func detectDatabase(svc ServiceEvidence, sig databaseSignature) (Finding, bool) {
	imageMatch := containsAny(strings.ToLower(svc.Image), sig.images)
	dataMount := dataMountFor(svc, sig)

	var (
		evidence   []Evidence
		confidence Confidence
	)

	// Confidence answers one question only: how sure are we that this service
	// *is* this engine. Whether its data can actually be reached is a separate
	// concern, carried by DataMount and its warning — conflating the two would
	// downgrade a certain PostgreSQL to "probable" merely because its volume is
	// declared elsewhere.
	switch {
	case imageMatch && (dataMount != nil || hasAny(svc.EnvironmentNames, sig.env)):
		// The image names the engine and something independent corroborates it:
		// either it stores the engine's files or it is configured like it.
		confidence = ConfidenceConfirmed
	case dataMount != nil && !sig.genericDataDir:
		// A custom image can still be caught by where it keeps its data.
		confidence = ConfidenceProbable
	case imageMatch:
		// The image name mentions the engine and nothing corroborates it. True
		// for a real database whose volume is declared elsewhere, and equally
		// true for an exporter, a migration tool or a backup sidecar.
		confidence = ConfidencePossible
	default:
		return Finding{}, false
	}

	if imageMatch {
		evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name,
			Detail: "image " + svc.Image + " matches " + sig.technology})
	}
	if dataMount != nil {
		evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name,
			Detail: fmt.Sprintf("%s is mounted at %s, where %s keeps its data",
				dataMount.Source, dataMount.Target, sig.technology)})
	}
	for _, env := range svc.EnvironmentNames {
		if equalAny(env, sig.env) {
			evidence = append(evidence, Evidence{Source: "compose", Subject: svc.Name,
				Detail: "environment key " + env + " is present"})
		}
	}

	warnings := []string{}
	if sig.technology == "redis" || sig.technology == "valkey" {
		warnings = append(warnings, "Persistence and cache-only intent cannot be inferred safely; confirm before enabling backup.")
	}
	if confidence == ConfidencePossible {
		warnings = append(warnings, "Only the image name suggests this. Tools such as exporters, "+
			"migration jobs and backup sidecars carry the engine's name without holding its data — "+
			"confirm before treating this as a database.")
	}
	if dataMount == nil && confidence != ConfidencePossible {
		warnings = append(warnings, "No data directory for this engine was found among the declared "+
			"mounts, so its files may not be captured by a storage backup at all.")
	}

	finding := Finding{
		ID: "database:" + svc.Name + ":" + sig.technology, Kind: "database",
		Technology: sig.technology, Service: svc.Name, Confidence: confidence,
		Evidence: evidence, Recommendation: sig.method, Consistency: sig.consistency,
		Warnings: warnings,
	}
	if dataMount != nil {
		finding.DataMount = &DataMount{Type: dataMount.Type, Source: dataMount.Source, Target: dataMount.Target}
	}
	return finding, true
}

func (databaseDetector) Detect(input Input) []Finding {
	var findings []Finding
	for _, svc := range input.Services {
		// When the image names an engine, that engine wins. MySQL and MariaDB
		// share /var/lib/mysql, so without this a MySQL service also reports a
		// probable MariaDB sitting on the same files — two databases where
		// there is one, and a backup plan that would dump the wrong one.
		imageIdentified := false
		for _, sig := range databaseSignatures {
			if containsAny(strings.ToLower(svc.Image), sig.images) {
				imageIdentified = true
				break
			}
		}

		for _, sig := range databaseSignatures {
			if imageIdentified && !containsAny(strings.ToLower(svc.Image), sig.images) {
				continue
			}
			if finding, found := detectDatabase(svc, sig); found {
				findings = append(findings, finding)
			}
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

func hasAny(values, wanted []string) bool {
	for _, value := range values {
		if equalAny(value, wanted) {
			return true
		}
	}
	return false
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
