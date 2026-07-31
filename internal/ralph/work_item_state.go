package ralph

import (
	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// persistWorkItemState applies the transition's field writes to the work item and
// persists the result. It is the single place a fire-and-forget state transition is
// written, so a new transition is added by naming its field writes here rather
// than by copying the mutate-then-save pair from whichever site happened to be
// nearby.
//
// The mutators are variadic because not every transition writes a field: the
// cancellation and usage-limit paths persist the state the item already carries,
// and the paths whose writes have to happen before a branch that reads them keep
// those writes at the call site and pass none. Everything else passes the writes
// it used to perform inline.
//
// It takes the store as a parameter rather than hanging off workItemRun because
// the scoper and the halt path reach it from their own receivers.
//
// KNOWN GAP: the save error is discarded. This is not a considered-safe decision -
// it is the behavior every call site had before this helper existed, preserved
// deliberately so this change is a pure move. A failed write leaves the item's
// on-disk state behind its in-memory state, and the next run resumes from the
// stale record without anyone being told. Fixing it belongs to the follow-up
// change request to 18_decompose-execute-work-item, which explicitly excludes
// changing this behavior; this is the one line that changes when it lands.
func persistWorkItemState(store *internal.YAMLStore, specID string, item *domain.WorkItem, mutators ...func(*domain.WorkItem)) {
	for _, mutate := range mutators {
		mutate(item)
	}
	_ = store.SaveWorkItemForSpec(specID, item)
}
