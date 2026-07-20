package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store persists events to the audit_events table.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Insert persists an event, assigning it an ID and CreatedAt if not already
// set.
func (s *Store) Insert(ctx context.Context, event Event) (Event, error) {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Metadata = redactMetadata(event.Metadata)

	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return Event{}, fmt.Errorf("encode event metadata: %w", err)
	}
	if event.Metadata == nil {
		metadataJSON = []byte("{}")
	}

	var actorUserID sql.NullString
	if event.ActorUserID != "" {
		actorUserID = sql.NullString{String: event.ActorUserID, Valid: true}
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO audit_events (id, actor_user_id, action, target_type, target_id, metadata_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.ID, actorUserID, string(event.Action), event.TargetType, event.TargetID,
		string(metadataJSON), event.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Event{}, fmt.Errorf("insert audit event: %w", err)
	}

	return event, nil
}

// ListRecent returns the most recent events, newest first, up to limit.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor_user_id, action, target_type, target_id, metadata_json, created_at
		 FROM audit_events ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	result := []Event{}
	for rows.Next() {
		var (
			event        Event
			actorUserID  sql.NullString
			metadataJSON string
			createdAt    string
		)
		if err := rows.Scan(&event.ID, &actorUserID, &event.Action, &event.TargetType, &event.TargetID, &metadataJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if actorUserID.Valid {
			event.ActorUserID = actorUserID.String
		}
		if metadataJSON != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &event.Metadata)
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}

	return result, nil
}
