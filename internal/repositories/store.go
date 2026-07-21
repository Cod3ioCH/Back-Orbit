package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cod3ioCH/Back-Orbit/internal/backup"
)

// store persists Repository rows.
type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) *store {
	return &store{db: db}
}

const repositoryColumns = `id, name, kind, location, status, last_error, last_checked_at, created_at, updated_at`

func (s *store) list(ctx context.Context) ([]Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("repositories: list: %w", err)
	}
	defer rows.Close()

	result := []Repository{}
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, repo)
	}
	return result, rows.Err()
}

func (s *store) get(ctx context.Context, id string) (Repository, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE id = ?`, id)
	return scanRepository(row)
}

func (s *store) insert(ctx context.Context, repo Repository) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repositories (`+repositoryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repo.ID, repo.Name, string(repo.Kind), repo.Location,
		string(repo.Status), repo.LastError, nullableTime(repo.LastCheckedAt),
		formatTime(repo.CreatedAt), formatTime(repo.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrNameTaken
		}
		return fmt.Errorf("repositories: insert: %w", err)
	}
	return nil
}

// updateStatus records the outcome of a check. Only the observation fields
// change, so a concurrent rename cannot be clobbered by a slow check finishing
// afterwards.
func (s *store) updateStatus(ctx context.Context, id string, status Status, lastError string, checkedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repositories SET status = ?, last_error = ?, last_checked_at = ?, updated_at = ? WHERE id = ?`,
		string(status), lastError, formatTime(checkedAt), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("repositories: update status: %w", err)
	}
	return nil
}

func (s *store) delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id); err != nil {
		return fmt.Errorf("repositories: delete: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (Repository, error) {
	var (
		repo          Repository
		kind          string
		status        string
		lastCheckedAt sql.NullString
		createdAt     string
		updatedAt     string
	)

	err := row.Scan(&repo.ID, &repo.Name, &kind, &repo.Location, &status,
		&repo.LastError, &lastCheckedAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("repositories: scan: %w", err)
	}

	repo.Kind = backup.RepositoryKind(kind)
	repo.Status = Status(status)
	repo.CreatedAt, _ = parseTime(createdAt)
	repo.UpdatedAt, _ = parseTime(updatedAt)
	if lastCheckedAt.Valid {
		if t, err := parseTime(lastCheckedAt.String); err == nil {
			repo.LastCheckedAt = &t
		}
	}
	return repo, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
