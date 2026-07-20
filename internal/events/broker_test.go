package events

import (
	"testing"
	"time"
)

func TestBrokerPublishDeliversToSubscriber(t *testing.T) {
	broker := NewBroker()
	ch, unsubscribe := broker.Subscribe()
	defer unsubscribe()

	broker.Publish(Event{Action: ActionLoginSucceeded})

	select {
	case event := <-ch:
		if event.Action != ActionLoginSucceeded {
			t.Fatalf("expected action %q, got %q", ActionLoginSucceeded, event.Action)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	broker := NewBroker()
	ch, unsubscribe := broker.Subscribe()
	unsubscribe()

	broker.Publish(Event{Action: ActionLoginSucceeded})

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after unsubscribe, not to receive an event")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected channel to be closed immediately after unsubscribe")
	}
}

func TestBrokerDoesNotBlockOnSlowSubscriber(t *testing.T) {
	broker := NewBroker()
	_, unsubscribe := broker.Subscribe() // never drained
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBufferSize*2; i++ {
			broker.Publish(Event{Action: ActionLoginSucceeded})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow/unread subscriber instead of dropping events")
	}
}

func TestBrokerMultipleSubscribers(t *testing.T) {
	broker := NewBroker()
	ch1, unsub1 := broker.Subscribe()
	defer unsub1()
	ch2, unsub2 := broker.Subscribe()
	defer unsub2()

	broker.Publish(Event{Action: ActionLogout})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case event := <-ch:
			if event.Action != ActionLogout {
				t.Fatalf("expected action %q, got %q", ActionLogout, event.Action)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event on one of the subscribers")
		}
	}
}
