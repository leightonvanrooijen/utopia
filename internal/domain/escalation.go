package domain

import "fmt"

// EscalationConfig bounds each escalation path the execution loop can take.
// Omit the section entirely to run with the built-in defaults, which is the
// pre-escalation behaviour.
//
// Example:
//
//	escalation:
//	  mechanical_retries: 4
//	  comprehension_escalations: 2
//	  opus_execution_attempts: 2
//	  scoping_escalations: 1
//
// Every cap is a pointer so an omitted key is distinguishable from an explicit
// zero. An omitted cap takes its default; a zero would disable the path it
// bounds rather than bound it, so validation rejects it instead of quietly
// reading it as "unset". That is the whole reason these are not plain ints like
// verification.max_iterations, where zero legitimately means unlimited.
//
// The caps are independently configurable because they are guesses: they were
// chosen so the worst case is roughly cost-neutral against running the
// expensive model throughout, and the telemetry that would confirm them does
// not exist yet.
type EscalationConfig struct {
	// MechanicalRetries caps consecutive mechanical retries on the same executor
	// before the failure is reclassified as comprehension.
	MechanicalRetries *int `yaml:"mechanical_retries,omitempty"`
	// ComprehensionEscalations caps comprehension failures before the work item
	// routes to scoping escalation instead of escalating the executor again.
	ComprehensionEscalations *int `yaml:"comprehension_escalations,omitempty"`
	// OpusExecutionAttempts caps how many execution attempts run on the escalated
	// executor. It is the bound that keeps the expensive path from costing more
	// than running the expensive model throughout would have.
	OpusExecutionAttempts *int `yaml:"opus_execution_attempts,omitempty"`
	// ScopingEscalations caps how many times one change request may be routed to
	// scoping escalation. It is the bound that stops rewrite-then-retry from
	// becoming an unbounded loop through a different door.
	ScopingEscalations *int `yaml:"scoping_escalations,omitempty"`
}

// CapOr returns the configured cap, or fallback when the key was omitted.
// Safe to call on a nil config, which is the omitted-section case.
func CapOr(configured *int, fallback int) int {
	if configured == nil {
		return fallback
	}
	return *configured
}

// ValidateEscalationConfig validates the escalation section of a config.
// Returns nil when the section is omitted or every configured cap is positive,
// and an error naming every offending key otherwise.
//
// It runs at config load time, alongside the model-name validation, so a cap
// that could never bound anything fails before a run starts rather than after
// the first work item has already spent money.
func ValidateEscalationConfig(ec *EscalationConfig) error {
	if ec == nil {
		return nil
	}

	var invalid []string
	check := func(configured *int, field string) {
		if configured != nil && *configured < 1 {
			invalid = append(invalid, fmt.Sprintf("escalation.%s: %d", field, *configured))
		}
	}

	check(ec.MechanicalRetries, "mechanical_retries")
	check(ec.ComprehensionEscalations, "comprehension_escalations")
	check(ec.OpusExecutionAttempts, "opus_execution_attempts")
	check(ec.ScopingEscalations, "scoping_escalations")

	if len(invalid) == 0 {
		return nil
	}

	return &InvalidEscalationCapError{InvalidFields: invalid}
}

// InvalidEscalationCapError indicates one or more non-positive escalation caps.
type InvalidEscalationCapError struct {
	InvalidFields []string
}

func (e *InvalidEscalationCapError) Error() string {
	return fmt.Sprintf("invalid escalation caps: %s (each cap bounds a retry path, so it must be at least 1 attempt; omit the key to use its default)",
		joinWithComma(e.InvalidFields))
}

// Is allows errors.Is to match any InvalidEscalationCapError.
func (e *InvalidEscalationCapError) Is(target error) bool {
	_, ok := target.(*InvalidEscalationCapError)
	return ok
}
