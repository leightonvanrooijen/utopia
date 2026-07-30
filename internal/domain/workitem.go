package domain

// Complexity indicates estimated effort for a work item
type Complexity string

const (
	ComplexityLow    Complexity = "low"
	ComplexityMedium Complexity = "medium"
	ComplexityHigh   Complexity = "high"
)

// CriteriaOrigin records where a split work item's acceptance criteria came
// from. It is empty for a work item that was not split, because the question
// only arises once a feature is divided: an unsplit item carries the feature's
// criteria verbatim, so there is nothing to attribute.
type CriteriaOrigin string

const (
	// CriteriaOriginPartitioned means the criteria were taken from the feature's
	// own acceptance criteria, each assigned to exactly one work item. Every
	// criterion stays traceable to the reviewed change request.
	CriteriaOriginPartitioned CriteriaOrigin = "partitioned"
	// CriteriaOriginAuthored means the sizer wrote the criteria itself, which it
	// may only do when a single criterion is too large to partition further. It is
	// recorded because criteria that no human reviewed should be visible rather
	// than silent.
	CriteriaOriginAuthored CriteriaOrigin = "authored"
)

// WorkItemStatus tracks execution state
type WorkItemStatus string

const (
	WorkItemPending    WorkItemStatus = "pending"
	WorkItemInProgress WorkItemStatus = "in_progress"
	WorkItemCompleted  WorkItemStatus = "completed"
	WorkItemFailed     WorkItemStatus = "failed"
	// WorkItemNeedsHuman means every bounded escalation path available to the item
	// is exhausted, so no further attempt can change the outcome. It is a distinct
	// terminal state rather than a synonym for failed because the operator action
	// differs: a failed item failed verification and can be retried as-is, while a
	// needs_human item is waiting for a person to re-scope or rewrite its change
	// request. Retrying it unchanged would exhaust the same caps again.
	WorkItemNeedsHuman WorkItemStatus = "needs_human"
)

// WorkItem represents a discrete unit of work for Ralph execution.
// Acceptance criteria are baked into the Prompt field, not stored separately.
// The completion token <COMPLETE> is part of the prompt template.
type WorkItem struct {
	ID      string         `yaml:"id"`
	SpecRef string         `yaml:"spec_ref"` // e.g., "user-authentication.signup"
	Title   string         `yaml:"title"`
	Status  WorkItemStatus `yaml:"status"`

	// The prompt that will be fed to Claude/Ralph (includes acceptance criteria)
	Prompt string `yaml:"prompt"`

	// Constraints that bound the implementation (e.g., "no new abstractions")
	Constraints []string `yaml:"constraints,omitempty"`

	// Sequential execution order (0-indexed position from spec)
	Order int `yaml:"order"`

	// SourceFeatureID names the feature this work item was derived from. It equals
	// the feature ID for an unsplit item, and stays the parent feature's ID for
	// every slice of a split one, whose own ID is suffixed to keep it unique. It is
	// what makes a split traceable: the item's ID identifies the slice, this
	// identifies the change request feature the slice belongs to.
	SourceFeatureID string `yaml:"source_feature_id,omitempty"`

	// CriteriaOrigin records whether a split item's criteria were partitioned from
	// the feature or authored by the sizer. Empty when the item was not split.
	CriteriaOrigin CriteriaOrigin `yaml:"criteria_origin,omitempty"`

	// Work items that must complete before this one
	Dependencies []string `yaml:"dependencies,omitempty"`

	// Estimated effort
	Complexity Complexity `yaml:"estimated_complexity"`

	// IterationCount tracks how many Claude invocations have been attempted.
	// Set when status is in_progress or completed.
	IterationCount int `yaml:"iteration_count,omitempty"`

	// LastFailureOutput stores the verification failure from the previous iteration.
	// Only the most recent failure is kept (not accumulated).
	LastFailureOutput string `yaml:"last_failure_output,omitempty"`

	// LastValidatorFeedback stores feedback from validators that failed in the previous iteration.
	// Kept separate from LastFailureOutput to allow distinct prompt sections.
	LastValidatorFeedback string `yaml:"last_validator_feedback,omitempty"`

	// FailureConclusions accumulates what each failed validation attempt
	// concluded: the failing validator's diagnosis and, on a comprehension
	// failure, the corrected intent. It is the one thing that survives a failed
	// attempt into an escalated one, because an escalated attempt is built a fresh
	// context from the evidence rather than handed the transcript that produced it
	// - replaying that transcript would anchor the escalated model to the mental
	// model that just failed. It is cleared when the change request is rewritten,
	// since the diagnoses were about a text that no longer exists.
	FailureConclusions []FailureConclusion `yaml:"failure_conclusions,omitempty"`

	// SelectedValidators holds the validator IDs the relevance router chose for
	// this work item. It is populated once, when the item first reaches the
	// validation gate, and reused across retries and resumes so the routing model
	// call happens once per work item rather than every iteration. See
	// ValidatorsRouted to distinguish "not yet routed" from "routed, selected none".
	SelectedValidators []string `yaml:"selected_validators,omitempty"`

	// MechanicalRetryCount tracks consecutive mechanical validation failures on
	// the default executor. It is persisted alongside IterationCount so the
	// mechanical retry cap holds across a resume, and it resets on any
	// comprehension failure because it counts slips in a row.
	MechanicalRetryCount int `yaml:"mechanical_retry_count,omitempty"`

	// ComprehensionCount tracks comprehension validation failures - the ones the
	// same executor cannot fix by trying harder. It is the escalation state: a
	// non-zero count means execution runs on the escalated executor, so persisting
	// it is what stops a resumed work item from resetting to the default executor.
	ComprehensionCount int `yaml:"comprehension_count,omitempty"`

	// OpusExecutionAttempts tracks how many execution attempts have run on the
	// escalated executor. It is persisted so the cap holds across a resume, and it
	// is never reset - not by a scoping escalation either - because it bounds total
	// spend on the expensive path rather than a streak.
	OpusExecutionAttempts int `yaml:"opus_execution_attempts,omitempty"`

	// ScopingEscalationCount tracks how many times this item has been routed to
	// scoping escalation. It is persisted so the cap holds across a resume: a
	// rewritten change request that fails the same way must not be rewritten
	// forever.
	ScopingEscalationCount int `yaml:"scoping_escalations,omitempty"`

	// MechanicalFailureTotal, ComprehensionFailureTotal and
	// ReclassifiedFailureTotal are the lifetime failure tallies by the class the
	// validators reported, as opposed to the counters above, which are routing
	// state and reset. They exist because the routing record needs the ratio of
	// mechanical to comprehension failures over the whole item, and every counter
	// that routing uses is reset by something: the mechanical one by a
	// comprehension failure, the comprehension one by a change-request rewrite.
	//
	// They are persisted with the item and never reset, so an item that resumes
	// mid-run does not report a truncated history.
	MechanicalFailureTotal    int `yaml:"mechanical_failures_total,omitempty"`
	ComprehensionFailureTotal int `yaml:"comprehension_failures_total,omitempty"`
	ReclassifiedFailureTotal  int `yaml:"reclassified_failures_total,omitempty"`

	// ExecutorAttempts is every execution attempt made on this item, with the model
	// and effort it ran on. It is persisted for the same reason the counters are -
	// so a resumed item reports its whole history - and it is what makes "no path
	// raised the default executor's effort" a claim a reader can check against the
	// record rather than only a test the loop passed once.
	//
	// Only attempts that actually ran are kept: one refunded for a usage limit is
	// dropped, exactly as its iteration and its escalation charge are.
	ExecutorAttempts []ExecutorAttempt `yaml:"executor_attempts,omitempty"`

	// SpecRewritten records that a scoping escalation produced a change request
	// execution actually resumed against. It is distinct from a non-zero
	// ScopingEscalationCount, which counts rewrites attempted including the ones
	// that produced nothing usable.
	SpecRewritten bool `yaml:"spec_rewritten,omitempty"`

	// ValidatorsRouted records whether the relevance router has already run for
	// this work item. Once true, SelectedValidators is authoritative even when
	// empty, so routing is not repeated on retries or after a resume.
	ValidatorsRouted bool `yaml:"validators_routed,omitempty"`
}

// Executor roles an attempt can run under. The role is recorded rather than
// inferred from the model string because a project may configure models.execute
// and models.execute_escalated to the same model and still have escalated - the
// role is what the caps bound and what the cost approximation counts.
const (
	ExecutorRoleDefault   = "default-executor"
	ExecutorRoleEscalated = "escalated-executor"
)

// ExecutorAttempt is one execution attempt's routing: which role ran it, on which
// model, at which effort. It carries no output - the transcript already holds
// that - only what the attempt cost by way of tier.
type ExecutorAttempt struct {
	// Iteration is the item's iteration number this attempt was.
	Iteration int `yaml:"iteration"`
	// Role is ExecutorRoleDefault or ExecutorRoleEscalated.
	Role string `yaml:"role"`
	// Model is the model the attempt ran on.
	Model string `yaml:"model"`
	// Effort is the resolved effort level, empty when none was configured and the
	// claude CLI default applied.
	Effort string `yaml:"effort,omitempty"`

	// Usage is what the attempt spent, as the claude CLI reported it. Model above is
	// the value routing configured - an alias like "opus" for most projects - while
	// Usage.Model is the model id the CLI resolved that alias to, which is what a
	// comparison across runs has to key on.
	//
	// Nil means no usage was captured for the attempt at all, which is every attempt
	// recorded before capture existed. Non-nil with Available false means the attempt
	// ran and its accounting could not be read. Neither is zero spend.
	Usage *AttemptUsage `yaml:"usage,omitempty"`
}

// FailureConclusion is one failing validator's conclusion about one attempt. It
// holds conclusions only - never the attempt's reasoning, its partial diffs or
// its tool calls - so carrying it into a later prompt cannot smuggle the failed
// attempt's transcript along with it.
type FailureConclusion struct {
	// Iteration is the attempt the conclusion was reached about.
	Iteration int `yaml:"iteration"`
	// ValidatorID names the validator that reached it.
	ValidatorID string `yaml:"validator_id"`
	// FailureClass is the class that validator reported, after normalization.
	FailureClass string `yaml:"failure_class,omitempty"`
	// Diagnosis is why the check failed.
	Diagnosis string `yaml:"diagnosis"`
	// CorrectedIntent is what the executor should have understood the work item to
	// mean, present on a comprehension failure and empty otherwise.
	CorrectedIntent string `yaml:"corrected_intent,omitempty"`
}

// NewWorkItem creates a work item from a spec feature.
// The prompt and constraints are set separately by the chunking strategy.
//
// SourceFeatureID defaults to featureID and CriteriaOrigin is left empty, which
// is the unsplit case. A caller generating slices of a split feature overwrites
// both: the parent feature's ID and how the slice's criteria were arrived at.
func NewWorkItem(id string, specID string, featureID string, feature Feature, order int) *WorkItem {
	return &WorkItem{
		ID:              id,
		SpecRef:         specID + "." + featureID,
		Title:           feature.Description,
		Status:          WorkItemPending,
		Order:           order,
		Complexity:      ComplexityMedium,
		SourceFeatureID: featureID,
	}
}
