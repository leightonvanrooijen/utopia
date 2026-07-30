package domain

import "fmt"

// DefaultTurnBudget is the per-iteration turn ceiling used when
// work_items.turn_budget is omitted. It is a guess, chosen to be generous
// enough that a normally-sized work item never hits it and low enough that a
// looping iteration stops costing money early.
const DefaultTurnBudget = 40

// WorkItemsConfig holds settings that belong to work items themselves rather
// than to any one command that handles them.
//
// Example:
//
//	work_items:
//	  turn_budget: 40
//
// The section is named after work items because both the chunker and the
// executor read it: the chunker sizes items against the budget, the executor
// enforces it as a hard cap. A flag on `execute` would be invisible to `chunk`,
// and a key under either command's section would misattribute the knob.
type WorkItemsConfig struct {
	// TurnBudget is the number of turns one execution iteration may spend, and
	// the size target the chunker cuts work items against. One number governs
	// both, so tuning cost means changing one value rather than keeping two in
	// sync.
	//
	// It is a pointer so an omitted key is distinguishable from an explicit
	// zero. Omitted takes DefaultTurnBudget; zero would mean an iteration that
	// may spend no turns at all, which is not a budget, so validation rejects it
	// rather than quietly reading it as "unset".
	TurnBudget *int `yaml:"turn_budget,omitempty"`
}

// TurnBudgetOr returns the configured turn budget, or DefaultTurnBudget when
// the work_items section or its turn_budget key was omitted.
// Safe to call on a nil config, which is the omitted-section case.
func (c *WorkItemsConfig) TurnBudgetOr() int {
	if c == nil || c.TurnBudget == nil {
		return DefaultTurnBudget
	}
	return *c.TurnBudget
}

// ValidateWorkItemsConfig validates the work_items section of a config.
// Returns nil when the section is omitted or turn_budget is positive, and an
// error naming the field otherwise.
//
// It runs at config load time, alongside the escalation cap validation, so a
// budget that could never bound anything fails before a run starts rather than
// after an iteration has already been launched with a nonsense cap.
func ValidateWorkItemsConfig(wc *WorkItemsConfig) error {
	if wc == nil || wc.TurnBudget == nil {
		return nil
	}
	if *wc.TurnBudget < 1 {
		return &InvalidTurnBudgetError{Value: *wc.TurnBudget}
	}
	return nil
}

// InvalidTurnBudgetError indicates a non-positive work_items.turn_budget.
type InvalidTurnBudgetError struct {
	Value int
}

func (e *InvalidTurnBudgetError) Error() string {
	return fmt.Sprintf("invalid work_items.turn_budget: %d (a budget bounds the turns one iteration may spend, so it must be at least 1; omit the key to use the default of %d)",
		e.Value, DefaultTurnBudget)
}

// Is allows errors.Is to match any InvalidTurnBudgetError.
func (e *InvalidTurnBudgetError) Is(target error) bool {
	_, ok := target.(*InvalidTurnBudgetError)
	return ok
}
