package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// store persists Record values in the projects table.
type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) *store {
	return &store{db: db}
}

func (s *store) list(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, compose_path, compose_files_json, source, status, created_at, updated_at
		 FROM projects ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		record, err := scanRecordRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *store) getByID(ctx context.Context, id string) (Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, compose_path, compose_files_json, source, status, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	)
	return scanRecordRow(row)
}

func (s *store) getByName(ctx context.Context, name string) (Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, compose_path, compose_files_json, source, status, created_at, updated_at
		 FROM projects WHERE name = ?`, name,
	)
	return scanRecordRow(row)
}

func (s *store) insert(ctx context.Context, record Record) error {
	composeFilesJSON, err := json.Marshal(record.ComposeFiles)
	if err != nil {
		return fmt.Errorf("encode compose files: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, compose_path, compose_files_json, source, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.Name, record.ComposePath, string(composeFilesJSON),
		string(record.Source), string(record.Status),
		record.CreatedAt.Format(time.RFC3339Nano), record.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrProjectExists
		}
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

// upsertDiscovered creates a project record if none exists for name, or
// refreshes its compose path/files/updated_at if one does. It never
// downgrades a manually registered project's Source, and never touches
// Status, which reflects backup coverage, not Docker liveness.
func (s *store) upsertDiscovered(ctx context.Context, name, composePath string, composeFiles []string) (Record, error) {
	existing, err := s.getByName(ctx, name)
	if err == nil {
		existing.ComposePath = composePath
		existing.ComposeFiles = composeFiles
		existing.UpdatedAt = time.Now().UTC()
		if updateErr := s.update(ctx, existing); updateErr != nil {
			return Record{}, updateErr
		}
		return existing, nil
	}
	if !errors.Is(err, ErrProjectNotFound) {
		return Record{}, err
	}

	now := time.Now().UTC()
	record := Record{
		ID:           uuid.NewString(),
		Name:         name,
		ComposePath:  composePath,
		ComposeFiles: composeFiles,
		Source:       SourceDiscovered,
		Status:       StatusUnprotected,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.insert(ctx, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *store) update(ctx context.Context, record Record) error {
	composeFilesJSON, err := json.Marshal(record.ComposeFiles)
	if err != nil {
		return fmt.Errorf("encode compose files: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE projects SET compose_path = ?, compose_files_json = ?, status = ?, updated_at = ? WHERE id = ?`,
		record.ComposePath, string(composeFilesJSON), string(record.Status),
		record.UpdatedAt.Format(time.RFC3339Nano), record.ID,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecordRow(row rowScanner) (Record, error) {
	var (
		record           Record
		composeFilesJSON string
		source           string
		status           string
		createdAt        string
		updatedAt        string
	)

	err := row.Scan(&record.ID, &record.Name, &record.ComposePath, &composeFilesJSON,
		&source, &status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrProjectNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("scan project: %w", err)
	}

	if composeFilesJSON != "" {
		_ = json.Unmarshal([]byte(composeFilesJSON), &record.ComposeFiles)
	}
	record.Source = Source(source)
	record.Status = Status(status)
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return record, nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
