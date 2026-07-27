package ralph

import "github.com/leightonvanrooijen/utopia/internal/domain"

// Lifecycle event names emitted by the execution loop, in loop order.
// The canonical definitions live in the domain package so config validation
// and the loop share one vocabulary; these aliases keep call sites local.
const (
	EventExecutionStarted           = domain.EventExecutionStarted
	EventWorkItemStarted            = domain.EventWorkItemStarted
	EventWorkItemCompletionClaimed  = domain.EventWorkItemCompletionClaimed
	EventWorkItemVerificationFailed = domain.EventWorkItemVerificationFailed
	EventWorkItemVerified           = domain.EventWorkItemVerified
	EventWorkItemCommitted          = domain.EventWorkItemCommitted
	EventPhaseVerified              = domain.EventPhaseVerified
	EventPhaseCompleted             = domain.EventPhaseCompleted
	EventExecutionCompleted         = domain.EventExecutionCompleted
	EventExecutionFailed            = domain.EventExecutionFailed
)

// EventPayload carries the structured context for a lifecycle event.
// CRID, CRTitle, and SpecID are set on every event. WorkItemID and
// IterationCount are set on workitem-scoped events, CommitSHA on
// post-commit events, and Reason on execution-failed.
type EventPayload struct {
	CRID           string `json:"cr_id"`
	CRTitle        string `json:"cr_title"`
	SpecID         string `json:"spec_id"`
	WorkItemID     string `json:"work_item_id,omitempty"`
	IterationCount int    `json:"iteration_count,omitempty"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// Event is a named lifecycle event emitted by the execution loop.
type Event struct {
	Name    string
	Payload EventPayload
}

// Dispatcher delivers lifecycle events to registered subscribers.
// With no subscribers registered, dispatching is a no-op.
type Dispatcher struct {
	subscribers []func(Event) error
}

// NewDispatcher creates a Dispatcher with no subscribers.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Subscribe registers fn to receive every dispatched event. A subscriber
// returns a non-nil error to block loop progression at gating events;
// fire-and-forget subscribers return nil.
func (d *Dispatcher) Subscribe(fn func(Event) error) {
	d.subscribers = append(d.subscribers, fn)
}

// Dispatch delivers the event to all subscribers in registration order and
// returns the first blocking error. Every subscriber receives the event even
// when an earlier one blocks, so side effects are not silently skipped.
func (d *Dispatcher) Dispatch(e Event) error {
	var firstErr error
	for _, fn := range d.subscribers {
		if err := fn(e); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
