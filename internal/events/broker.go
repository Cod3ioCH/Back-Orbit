package events

import (
	"log/slog"
	"sync"
)

// subscriberBufferSize is how many unread events a slow SSE subscriber can
// fall behind by before Broker starts dropping events for it, rather than
// blocking every publisher on one slow client.
const subscriberBufferSize = 64

// Broker is an in-process publish/subscribe hub for live events, feeding
// Server-Sent Event streams to connected browsers. It holds no persistent
// state; persistence is Store's job.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[int64]chan Event
	nextID      int64
}

// NewBroker creates an empty Broker.
func NewBroker() *Broker {
	return &Broker{subscribers: make(map[int64]chan Event)}
}

// Subscribe registers a new subscriber and returns a channel of future
// events plus an unsubscribe function that the caller must call (typically
// via defer) once done, e.g. when the SSE client disconnects.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	ch := make(chan Event, subscriberBufferSize)
	b.subscribers[id] = ch

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(ch)
		}
	}

	return ch, unsubscribe
}

// Publish delivers event to every current subscriber. Delivery is
// best-effort and non-blocking: a subscriber that isn't keeping up has
// events dropped for it rather than stalling the publisher (which would
// otherwise let one slow browser tab back up every audit-relevant action in
// the system).
func (b *Broker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			slog.Warn("events: dropping event for slow subscriber", "action", event.Action)
		}
	}
}
