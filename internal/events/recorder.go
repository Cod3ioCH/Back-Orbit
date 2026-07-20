package events

import (
	"context"
	"log/slog"
)

// Recorder is the single entry point application code should use to emit an
// event: it persists the event to the audit log and publishes it to live
// SSE subscribers. Using Recorder instead of Store/Broker directly ensures
// every emitted event is both durable and visible live, without callers
// having to remember to do both.
type Recorder struct {
	store  *Store
	broker *Broker
}

// NewRecorder creates a Recorder backed by store and broker.
func NewRecorder(store *Store, broker *Broker) *Recorder {
	return &Recorder{store: store, broker: broker}
}

// Emit persists and publishes event. Persistence errors are logged but
// intentionally not returned: an audit-trail write failure must never abort
// the user-facing action that triggered it (e.g. a failed login should still
// be rejected even if the audit insert itself fails), but it must be loud in
// the logs since a silently-failing audit log defeats its purpose.
func (r *Recorder) Emit(ctx context.Context, event Event) {
	stored, err := r.store.Insert(ctx, event)
	if err != nil {
		slog.Error("events: failed to persist audit event", "action", event.Action, "error", err)
		// Still publish the (unpersisted) event so the live activity feed
		// doesn't silently miss it too.
		stored = event
	}
	r.broker.Publish(stored)
}
