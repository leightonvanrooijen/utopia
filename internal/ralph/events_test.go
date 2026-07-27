package ralph

import (
	"errors"
	"testing"
)

func TestDispatcher_NoSubscribers(t *testing.T) {
	d := NewDispatcher()

	// Dispatching with no subscribers must be a safe no-op
	d.Dispatch(Event{Name: EventExecutionStarted, Payload: EventPayload{CRID: "cr-1"}})
}

func TestDispatcher_DeliversToAllSubscribersInOrder(t *testing.T) {
	d := NewDispatcher()

	var order []string
	d.Subscribe(func(e Event) error {
		order = append(order, "first:"+e.Name)
		return nil
	})
	d.Subscribe(func(e Event) error {
		order = append(order, "second:"+e.Name)
		return nil
	})

	d.Dispatch(Event{Name: EventWorkItemStarted})
	d.Dispatch(Event{Name: EventWorkItemVerified})

	want := []string{
		"first:" + EventWorkItemStarted,
		"second:" + EventWorkItemStarted,
		"first:" + EventWorkItemVerified,
		"second:" + EventWorkItemVerified,
	}
	if len(order) != len(want) {
		t.Fatalf("got %d deliveries, want %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("delivery %d = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestDispatcher_SubscriberReceivesPayload(t *testing.T) {
	d := NewDispatcher()

	var got Event
	d.Subscribe(func(e Event) error { got = e; return nil })

	sent := Event{
		Name: EventWorkItemCommitted,
		Payload: EventPayload{
			CRID:           "my-cr",
			CRTitle:        "My change request",
			SpecID:         "my-cr/phase-0",
			WorkItemID:     "work-item-1",
			IterationCount: 3,
			CommitSHA:      "abc123",
		},
	}
	d.Dispatch(sent)

	if got != sent {
		t.Errorf("subscriber received %+v, want %+v", got, sent)
	}
}

func TestDispatcher_ReturnsFirstBlockingErrorAndStillDeliversToAll(t *testing.T) {
	d := NewDispatcher()

	first := errors.New("first block")
	second := errors.New("second block")
	var delivered []string
	d.Subscribe(func(e Event) error {
		delivered = append(delivered, "blocker-1")
		return first
	})
	d.Subscribe(func(e Event) error {
		delivered = append(delivered, "notify")
		return nil
	})
	d.Subscribe(func(e Event) error {
		delivered = append(delivered, "blocker-2")
		return second
	})

	err := d.Dispatch(Event{Name: EventWorkItemVerified})

	if err != first {
		t.Errorf("Dispatch returned %v, want first blocking error %v", err, first)
	}
	if len(delivered) != 3 {
		t.Errorf("expected all 3 subscribers to receive the event despite blocking, got %v", delivered)
	}
}

func TestDispatcher_NilErrorWhenNoSubscriberBlocks(t *testing.T) {
	d := NewDispatcher()
	d.Subscribe(func(e Event) error { return nil })

	if err := d.Dispatch(Event{Name: EventPhaseVerified}); err != nil {
		t.Errorf("expected nil error when no subscriber blocks, got %v", err)
	}
}
