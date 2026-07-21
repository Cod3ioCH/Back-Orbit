package projectanalyzer

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Cod3ioCH/Back-Orbit/internal/docker"
	"github.com/Cod3ioCH/Back-Orbit/internal/events"
	"github.com/Cod3ioCH/Back-Orbit/internal/projects"
	"github.com/Cod3ioCH/Back-Orbit/internal/protectionblueprints"
)

var ErrBlueprintNotFound = errors.New("project analyzer: blueprint not found")

type Service struct {
	db         *sql.DB
	projects   *projects.Service
	detectors  []ProjectDetector
	recorder   *events.Recorder
	docker     docker.Client
	catalog    protectionblueprints.Catalog
	catalogErr error
}

func NewService(db *sql.DB, projectService *projects.Service, dockerClient docker.Client, recorder *events.Recorder) *Service {
	catalog, catalogErr := protectionblueprints.LoadBuiltin()
	return &Service{db: db, projects: projectService, detectors: DefaultDetectors(), recorder: recorder, docker: dockerClient, catalog: catalog, catalogErr: catalogErr}
}

func (s *Service) Analyze(ctx context.Context, projectID, actorID string) (Blueprint, error) {
	detail, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Blueprint{}, err
	}
	input := Input{ProjectID: projectID}
	input.Warnings = append(input.Warnings, composeEvidence(ctx, detail, &input)...)
	mergeRuntime(detail, &input)
	input.SQLite, input.Warnings = findSQLite(ctx, detail.ComposePath, input.Warnings)
	mountedSQLite, mountWarnings := findSQLiteInSources(ctx, s.docker, detail.Sources)
	input.SQLite = uniqueSorted(append(input.SQLite, mountedSQLite...))
	input.Warnings = append(input.Warnings, mountWarnings...)
	var sqliteArtifacts []string
	input.SQLite, sqliteArtifacts = classifySQLitePaths(input.SQLite)
	if len(sqliteArtifacts) > 0 {
		input.Warnings = append(input.Warnings, fmt.Sprintf("%d SQLite backup or archive copies were detected and excluded from active databases.", len(sqliteArtifacts)))
	}

	// Services are collected out of a Compose document's service map, whose
	// iteration order Go deliberately randomises. Left unsorted they reach the
	// fingerprint in a different order every run, so a project nobody touched
	// reports itself as drifted — and a drift signal that fires at random
	// teaches people to click past it. Sorting also makes template matching
	// reproducible, since it decides which image fills which role.
	sort.Slice(input.Services, func(i, j int) bool {
		if input.Services[i].Name == input.Services[j].Name {
			return input.Services[i].Image < input.Services[j].Image
		}
		return input.Services[i].Name < input.Services[j].Name
	})

	findings := []Finding{}
	for _, detector := range s.detectors {
		if err := ctx.Err(); err != nil {
			return Blueprint{}, err
		}
		findings = append(findings, detector.Detect(input)...)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	templateMatches := []protectionblueprints.Result{}
	if s.catalogErr != nil {
		input.Warnings = append(input.Warnings, "Built-in protection templates could not be loaded; application-specific recommendations are unavailable.")
	} else {
		images := make([]string, 0, len(input.Services))
		technologies := make([]string, 0)
		for _, service := range input.Services {
			images = append(images, service.Image)
		}
		for _, finding := range findings {
			if finding.Kind == "database" {
				technologies = append(technologies, finding.Technology)
			}
		}
		templateMatches = s.catalog.Match(protectionblueprints.Evidence{Images: images, Technologies: technologies})
	}
	fingerprint, err := fingerprintAnalysis(input, findings, templateMatches)
	if err != nil {
		return Blueprint{}, err
	}
	confirmedFingerprint, confirmedAt, previousErr := s.confirmation(ctx, projectID)
	if previousErr != nil && !errors.Is(previousErr, ErrBlueprintNotFound) {
		return Blueprint{}, previousErr
	}
	now := time.Now().UTC()
	bp := Blueprint{SchemaVersion: SchemaVersion, ProjectID: projectID, Fingerprint: fingerprint, AnalyzedAt: now, ConfirmedAt: confirmedAt, Drifted: confirmedFingerprint != "" && confirmedFingerprint != fingerprint, Findings: findings, Steps: plan(findings), Warnings: uniqueSorted(input.Warnings), TemplateMatches: templateMatches}
	if err := s.save(ctx, bp, confirmedFingerprint, confirmedAt); err != nil {
		return Blueprint{}, err
	}
	s.recorder.Emit(ctx, events.Event{Action: events.ActionProjectAnalyzed, ActorUserID: actorID, TargetType: "project", TargetID: projectID, Metadata: map[string]any{"findings": len(findings), "warnings": len(bp.Warnings), "drifted": bp.Drifted}})
	return bp, nil
}

func (s *Service) confirmation(ctx context.Context, projectID string) (string, *time.Time, error) {
	var fp, at sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT confirmed_fingerprint,confirmed_at FROM project_blueprints WHERE project_id=?`, projectID).Scan(&fp, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrBlueprintNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("load blueprint confirmation: %w", err)
	}
	var parsed *time.Time
	if at.Valid {
		t, err := time.Parse(time.RFC3339Nano, at.String)
		if err == nil {
			parsed = &t
		}
	}
	return fp.String, parsed, nil
}

func (s *Service) Get(ctx context.Context, projectID string) (Blueprint, error) {
	var raw, analyzed, confirmed sql.NullString
	var version int
	var fp string
	err := s.db.QueryRowContext(ctx, `SELECT schema_version,fingerprint,analysis_json,analyzed_at,confirmed_at FROM project_blueprints WHERE project_id=?`, projectID).Scan(&version, &fp, &raw, &analyzed, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return Blueprint{}, ErrBlueprintNotFound
	}
	if err != nil {
		return Blueprint{}, fmt.Errorf("load project blueprint: %w", err)
	}
	var bp Blueprint
	if err := json.Unmarshal([]byte(raw.String), &bp); err != nil {
		return Blueprint{}, fmt.Errorf("decode project blueprint: %w", err)
	}
	if confirmed.Valid {
		t, err := time.Parse(time.RFC3339Nano, confirmed.String)
		if err == nil {
			bp.ConfirmedAt = &t
		}
	}
	return bp, nil
}

func (s *Service) Confirm(ctx context.Context, projectID, actorID string) (Blueprint, error) {
	bp, err := s.Get(ctx, projectID)
	if err != nil {
		return Blueprint{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE project_blueprints SET confirmed_fingerprint=fingerprint,confirmed_at=? WHERE project_id=?`, now.Format(time.RFC3339Nano), projectID)
	if err != nil {
		return Blueprint{}, fmt.Errorf("confirm project blueprint: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Blueprint{}, ErrBlueprintNotFound
	}
	bp.ConfirmedAt = &now
	bp.Drifted = false
	s.recorder.Emit(ctx, events.Event{Action: events.ActionProjectBlueprintConfirmed, ActorUserID: actorID, TargetType: "project", TargetID: projectID, Metadata: map[string]any{"fingerprint": bp.Fingerprint}})
	return bp, nil
}

func (s *Service) save(ctx context.Context, bp Blueprint, confirmedFP string, confirmedAt *time.Time) error {
	raw, err := json.Marshal(bp)
	if err != nil {
		return fmt.Errorf("encode project blueprint: %w", err)
	}
	var confirmed any
	if confirmedAt != nil {
		confirmed = confirmedAt.Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO project_blueprints(project_id,schema_version,fingerprint,analysis_json,analyzed_at,confirmed_fingerprint,confirmed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET schema_version=excluded.schema_version,fingerprint=excluded.fingerprint,analysis_json=excluded.analysis_json,analyzed_at=excluded.analyzed_at,confirmed_fingerprint=CASE WHEN project_blueprints.confirmed_fingerprint=excluded.fingerprint THEN project_blueprints.confirmed_fingerprint ELSE project_blueprints.confirmed_fingerprint END,confirmed_at=project_blueprints.confirmed_at`, bp.ProjectID, bp.SchemaVersion, bp.Fingerprint, string(raw), bp.AnalyzedAt.Format(time.RFC3339Nano), nullable(confirmedFP), confirmed)
	if err != nil {
		return fmt.Errorf("save project blueprint: %w", err)
	}
	return nil
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func fingerprint(findings []Finding) (string, error) {
	raw, err := json.Marshal(findings)
	if err != nil {
		return "", fmt.Errorf("fingerprint findings: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func fingerprintAnalysis(input Input, findings []Finding, matches []protectionblueprints.Result) (string, error) {
	safe := struct {
		Services        []ServiceEvidence             `json:"services"`
		Secrets         []string                      `json:"secrets"`
		Configs         []string                      `json:"configs"`
		EnvFiles        []string                      `json:"envFiles"`
		SQLite          []string                      `json:"sqlite"`
		Findings        []Finding                     `json:"findings"`
		TemplateMatches []protectionblueprints.Result `json:"templateMatches"`
	}{input.Services, input.Secrets, input.Configs, input.EnvFiles, input.SQLite, findings, matches}
	raw, err := json.Marshal(safe)
	if err != nil {
		return "", fmt.Errorf("fingerprint analysis: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func plan(findings []Finding) []Step {
	steps := []Step{{Order: 1, Action: "validate", Description: "Validate Compose files, source paths, repository access, and required secret mappings."}}
	hasDB, hasStorage := false, false
	for _, f := range findings {
		hasDB = hasDB || f.Kind == "database"
		hasStorage = hasStorage || f.Kind == "storage"
	}
	if hasDB {
		steps = append(steps, Step{Action: "dump_databases", Description: "Create and validate logical database dumps using credentials from the Secret Store."})
	}
	if hasStorage {
		steps = append(steps, Step{Action: "capture_storage", Description: "Capture selected named volumes and bind mounts while preserving metadata."})
	}
	steps = append(steps, Step{Action: "snapshot", Description: "Create an encrypted restic snapshot with the project manifest."}, Step{Action: "verify", Description: "Read back manifest and selected data, then apply retention only after verification."})
	for i := range steps {
		steps[i].Order = i + 1
	}
	return steps
}

type composeDocument struct {
	Services map[string]composeService `yaml:"services"`
	Secrets  map[string]any            `yaml:"secrets"`
	Configs  map[string]any            `yaml:"configs"`
}
type composeService struct {
	Image       string `yaml:"image"`
	Environment any    `yaml:"environment"`
	Volumes     []any  `yaml:"volumes"`
	Secrets     []any  `yaml:"secrets"`
	Configs     []any  `yaml:"configs"`
	EnvFile     any    `yaml:"env_file"`
}

func composeEvidence(ctx context.Context, detail projects.Detail, input *Input) []string {
	warnings := []string{}
	root, err := filepath.Abs(detail.ComposePath)
	if err != nil || root == "" {
		return []string{"Compose project path is unavailable; file analysis was skipped."}
	}
	for _, name := range detail.ComposeFiles {
		if err := ctx.Err(); err != nil {
			return append(warnings, "Analysis was cancelled.")
		}
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			warnings = append(warnings, "A Compose file outside the registered project path was not read: "+filepath.Base(p))
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			warnings = append(warnings, "Could not read Compose file "+filepath.Base(p)+".")
			continue
		}
		if len(data) > 4<<20 {
			warnings = append(warnings, "Compose file is larger than the 4 MiB analysis limit: "+filepath.Base(p))
			continue
		}
		var doc composeDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			warnings = append(warnings, "Could not parse Compose file "+filepath.Base(p)+".")
			continue
		}
		for svcName, svc := range doc.Services {
			se := ServiceEvidence{Name: svcName, Image: svc.Image, EnvironmentNames: environmentNames(svc.Environment), Mounts: mounts(svc.Volumes)}
			input.Services = mergeService(input.Services, se)
			input.Secrets = append(input.Secrets, referenceNames(svc.Secrets)...)
			input.Configs = append(input.Configs, referenceNames(svc.Configs)...)
			input.EnvFiles = append(input.EnvFiles, envFileNames(svc.EnvFile)...)
		}
		for n := range doc.Secrets {
			input.Secrets = append(input.Secrets, n)
		}
		for n := range doc.Configs {
			input.Configs = append(input.Configs, n)
		}
	}
	input.Secrets = uniqueSorted(input.Secrets)
	input.Configs = uniqueSorted(input.Configs)
	if _, err := os.Stat(filepath.Join(root, ".env")); err == nil {
		input.EnvFiles = append(input.EnvFiles, ".env")
	}
	input.EnvFiles = uniqueSorted(input.EnvFiles)
	return warnings
}

func mergeRuntime(detail projects.Detail, input *Input) {
	for _, c := range detail.Containers {
		se := ServiceEvidence{Name: c.Service, Image: c.Image}
		if se.Name == "" {
			se.Name = c.Name
		}
		for _, m := range c.Mounts {
			se.Mounts = append(se.Mounts, MountEvidence{Type: string(m.Type), Source: first(m.Name, m.Source), Target: m.Destination})
		}
		input.Services = mergeService(input.Services, se)
	}
}
func mergeService(all []ServiceEvidence, in ServiceEvidence) []ServiceEvidence {
	for i := range all {
		if all[i].Name == in.Name {
			if all[i].Image == "" {
				all[i].Image = in.Image
			}
			all[i].EnvironmentNames = uniqueSorted(append(all[i].EnvironmentNames, in.EnvironmentNames...))
			all[i].Mounts = append(all[i].Mounts, in.Mounts...)
			return all
		}
	}
	return append(all, in)
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func environmentNames(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k := range x {
			out = append(out, k)
		}
	case []any:
		for _, item := range x {
			s := fmt.Sprint(item)
			if i := strings.IndexByte(s, '='); i >= 0 {
				s = s[:i]
			}
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return uniqueSorted(out)
}
func referenceNames(values []any) []string {
	var out []string
	for _, v := range values {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			if s, ok := x["source"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func envFileNames(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, item := range x {
			switch value := item.(type) {
			case string:
				out = append(out, value)
			case map[string]any:
				if p, ok := value["path"].(string); ok {
					out = append(out, p)
				}
			}
		}
		return out
	}
	return nil
}
func mounts(values []any) []MountEvidence {
	var out []MountEvidence
	for _, v := range values {
		switch x := v.(type) {
		case string:
			parts := strings.Split(x, ":")
			if len(parts) >= 2 {
				kind := "bind"
				if !strings.HasPrefix(parts[0], ".") && !strings.HasPrefix(parts[0], "/") {
					kind = "volume"
				}
				out = append(out, MountEvidence{Type: kind, Source: parts[0], Target: parts[1]})
			}
		case map[string]any:
			out = append(out, MountEvidence{Type: fmt.Sprint(x["type"]), Source: fmt.Sprint(x["source"]), Target: fmt.Sprint(x["target"])})
		}
	}
	return out
}

func findSQLite(ctx context.Context, root string, warnings []string) ([]string, []string) {
	if root == "" {
		return nil, warnings
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, append(warnings, "Project path could not be normalized for SQLite discovery.")
	}
	found := []string{}
	count := 0
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 6 {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > 10000 {
			return filepath.SkipAll
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		header := make([]byte, 16)
		_, err = io.ReadFull(f, header)
		if err == nil && string(header) == "SQLite format 3\x00" {
			found = append(found, rel)
		}
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		warnings = append(warnings, "SQLite discovery was cancelled.")
	}
	if count > 10000 {
		warnings = append(warnings, "SQLite discovery stopped after 10,000 files.")
	}
	return uniqueSorted(found), warnings
}

func findSQLiteInSources(ctx context.Context, client docker.Client, sources []projects.BackupSource) ([]string, []string) {
	if client == nil || len(sources) == 0 {
		return nil, nil
	}
	image, err := client.SelfImage(ctx)
	if err != nil {
		return nil, []string{"Mounted storage could not be inspected for SQLite databases."}
	}
	var found, warnings []string
	for _, source := range sources {
		if !source.Backupable() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return found, append(warnings, "Mounted storage inspection was cancelled.")
		}
		containerID, err := client.CreateHelperContainer(ctx, docker.HelperContainerRequest{Image: image, Source: source.Name, MountPath: "/data", Purpose: "project-analysis"})
		if err != nil {
			warnings = append(warnings, "Could not inspect "+string(source.Kind)+" "+source.Name+".")
			continue
		}
		func() {
			defer func() {
				if err := client.RemoveContainer(context.WithoutCancel(ctx), containerID); err != nil {
					warnings = append(warnings, "Could not remove the storage analysis helper.")
				}
			}()
			archive, err := client.ContainerArchive(ctx, containerID, "/data/.")
			if err != nil {
				warnings = append(warnings, "Could not read "+string(source.Kind)+" "+source.Name+".")
				return
			}
			defer archive.Close()
			tr := tar.NewReader(archive)
			for entries := 0; ; entries++ {
				header, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					warnings = append(warnings, "Mounted storage "+source.Name+" returned an invalid archive.")
					break
				}
				if entries >= 10000 {
					warnings = append(warnings, "SQLite inspection of "+source.Name+" stopped after 10,000 entries.")
					break
				}
				if header.Typeflag != tar.TypeReg || !sqliteCandidate(header.Name) {
					continue
				}
				buf := make([]byte, 16)
				if _, err := io.ReadFull(tr, buf); err == nil && string(buf) == "SQLite format 3\x00" {
					found = append(found, string(source.Kind)+":"+source.Name+"/"+strings.TrimPrefix(filepath.Clean(header.Name), "./"))
				}
			}
		}()
	}
	return uniqueSorted(found), warnings
}

func sqliteCandidate(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".sqlite3")
}

func classifySQLitePaths(paths []string) (active, artifacts []string) {
	for _, value := range paths {
		normalized := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
		isArtifact := false
		for _, component := range strings.Split(normalized, "/") {
			switch component {
			case "backup", "backups", "archive", "archives", "snapshot", "snapshots":
				isArtifact = true
			}
		}
		if isArtifact {
			artifacts = append(artifacts, value)
		} else {
			active = append(active, value)
		}
	}
	return uniqueSorted(active), uniqueSorted(artifacts)
}
func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, v := range in {
		if v != "" {
			m[v] = true
		}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
