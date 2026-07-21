// Package events implements Back-Orbit's audit trail and live activity feed:
// every security-relevant or user-visible action is recorded as an Event,
// which is both persisted (for the audit log) and published to any live
// subscribers (for Server-Sent Events in the UI).
package events

import (
	"strings"
	"time"
)

// Action identifies the kind of event. Values are stable strings so they
// remain meaningful in the persisted audit log across versions.
type Action string

const (
	ActionAdminAccountCreated Action = "auth.admin_account_created"
	ActionLoginSucceeded      Action = "auth.login_succeeded"
	ActionLoginFailed         Action = "auth.login_failed"
	ActionLogout              Action = "auth.logout"

	ActionProjectRegistered         Action = "project.registered"
	ActionProjectUpdated            Action = "project.updated"
	ActionProjectRemoved            Action = "project.removed"
	ActionProjectScanned            Action = "project.scanned"
	ActionProjectAnalyzed           Action = "project.analyzed"
	ActionProjectBlueprintConfirmed Action = "project.blueprint_confirmed"

	// Secret store transitions are security-relevant in themselves: an unlock
	// is the moment every stored credential becomes readable, so when it
	// happened and who caused it belongs in the audit trail.
	ActionSecretStoreInitialized Action = "secrets.store_initialized"
	ActionSecretStoreUnlocked    Action = "secrets.store_unlocked"
	ActionSecretStoreLocked      Action = "secrets.store_locked"

	ActionRepositoryCreated     Action = "repository.created"
	ActionRepositoryDeleted     Action = "repository.deleted"
	ActionRepositoryInitialized Action = "repository.initialized"
	ActionRepositoryChecked     Action = "repository.checked"

	ActionBackupStarted    Action = "backup.started"
	ActionBackupCompleted  Action = "backup.completed"
	ActionBackupFailed     Action = "backup.failed"
	ActionBackupCancelled  Action = "backup.cancelled"
	ActionRestoreStarted   Action = "restore.started"
	ActionRestoreCompleted Action = "restore.completed"
	ActionRestoreFailed    Action = "restore.failed"
	ActionRestoreCancelled Action = "restore.cancelled"
)

// Event is a single audit/activity record.
type Event struct {
	ID          string         `json:"id"`
	Action      Action         `json:"action"`
	ActorUserID string         `json:"actorUserId,omitempty"`
	TargetType  string         `json:"targetType,omitempty"`
	TargetID    string         `json:"targetId,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// sensitiveMetadataKeys are stripped from event metadata before it is
// persisted or published, as a defense-in-depth measure: no event source in
// Back-Orbit should be attaching secrets to audit metadata in the first
// place, but this ensures a mistake there can never leak a credential
// through the audit log or activity stream.
var sensitiveMetadataKeys = map[string]bool{
	"password":    true,
	"secret":      true,
	"token":       true,
	"credential":  true,
	"credentials": true,
	"apikey":      true,
	"api_key":     true,
}

func redactMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	redacted := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if sensitiveMetadataKeys[strings.ToLower(k)] {
			redacted[k] = "[redacted]"
			continue
		}
		redacted[k] = v
	}
	return redacted
}
