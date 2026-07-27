package ralph

// Lifecycle event names emitted by the execution loop, in loop order.
const (
	EventExecutionStarted   = "execution-started"
	EventWorkItemStarted    = "workitem-started"
	EventWorkItemVerified   = "workitem-verified"
	EventWorkItemCommitted  = "workitem-committed"
	EventPhaseVerified      = "phase-verified"
	EventPhaseCompleted     = "phase-completed"
	EventExecutionCompleted = "execution-completed"
	EventExecutionFailed    = "execution-failed"
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
	subscribers []func(Event)
}

// NewDispatcher creates a Dispatcher with no subscribers.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Subscribe registers fn to receive every dispatched event.
func (d *Dispatcher) Subscribe(fn func(Event)) {
	d.subscribers = append(d.subscribers, fn)
}

// Dispatch delivers the event to all subscribers in registration order.
func (d *Dispatcher) Dispatch(e Event) {
	for _, fn := range d.subscribers {
		fn(e)
	}
}
