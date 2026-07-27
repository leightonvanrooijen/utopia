package ralph

import "testing"

func TestDispatcher_NoSubscribers(t *testing.T) {
	d := NewDispatcher()

	// Dispatching with no subscribers must be a safe no-op
	d.Dispatch(Event{Name: EventExecutionStarted, Payload: EventPayload{CRID: "cr-1"}})
}

func TestDispatcher_DeliversToAllSubscribersInOrder(t *testing.T) {
	d := NewDispatcher()

	var order []string
	d.Subscribe(func(e Event) {
		order = append(order, "first:"+e.Name)
	})
	d.Subscribe(func(e Event) {
		order = append(order, "second:"+e.Name)
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
	d.Subscribe(func(e Event) { got = e })

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
