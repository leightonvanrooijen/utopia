package domain

// RefactorStatus represents the lifecycle state of a refactor
type RefactorStatus string

const (
	RefactorStatusDraft      RefactorStatus = "draft"
	RefactorStatusReady      RefactorStatus = "ready"
	RefactorStatusInProgress RefactorStatus = "in-progress"
	RefactorStatusComplete   RefactorStatus = "complete"
)

// Refactor represents a code restructuring document
// Refactors are standalone (not linked to specs) and focus on HOW code is structured
// rather than WHAT it does. They are temporary work artifacts that are deleted after completion.
type Refactor struct {
	ID     string         `yaml:"id"`
	Title  string         `yaml:"title"`
	Status RefactorStatus `yaml:"status"`
	Tasks  []RefactorTask `yaml:"tasks"`
}

// RefactorTask represents a single task within a refactor
type RefactorTask struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}

// NewRefactor creates a new refactor with sensible defaults
func NewRefactor(id, title string) *Refactor {
	return &Refactor{
		ID:     id,
		Title:  title,
		Status: RefactorStatusDraft,
		Tasks:  []RefactorTask{},
	}
}

// AddTask adds a task to the refactor
func (r *Refactor) AddTask(t RefactorTask) {
	r.Tasks = append(r.Tasks, t)
}

// MarkReady transitions a draft refactor to ready status.
// Returns an error if the refactor is not in draft status.
func (r *Refactor) MarkReady() error {
	if r.Status != RefactorStatusDraft {
		return &InvalidStatusTransitionError{
			From: r.Status,
			To:   RefactorStatusReady,
		}
	}
	r.Status = RefactorStatusReady
	return nil
}

// MarkInProgress transitions a ready refactor to in-progress status.
// Returns an error if the refactor is not in ready status.
func (r *Refactor) MarkInProgress() error {
	if r.Status != RefactorStatusReady {
		return &InvalidStatusTransitionError{
			From: r.Status,
			To:   RefactorStatusInProgress,
		}
	}
	r.Status = RefactorStatusInProgress
	return nil
}

// MarkComplete transitions an in-progress refactor to complete status.
// Returns an error if the refactor is not in in-progress status.
// Note: Completed refactors should be deleted after successful verification.
func (r *Refactor) MarkComplete() error {
	if r.Status != RefactorStatusInProgress {
		return &InvalidStatusTransitionError{
			From: r.Status,
			To:   RefactorStatusComplete,
		}
	}
	r.Status = RefactorStatusComplete
	return nil
}

// InvalidStatusTransitionError represents an invalid status transition attempt
type InvalidStatusTransitionError struct {
	From RefactorStatus
	To   RefactorStatus
}

func (e *InvalidStatusTransitionError) Error() string {
	return "invalid status transition from " + string(e.From) + " to " + string(e.To)
}

// ValidateRefactor checks that a refactor has all required fields populated correctly.
// Returns nil if valid, or an error describing what's missing/invalid.
func ValidateRefactor(r *Refactor) error {
	var errors []string

	if r.ID == "" {
		errors = append(errors, "missing required field: id")
	}
	if r.Title == "" {
		errors = append(errors, "missing required field: title")
	}
	if r.Status == "" {
		errors = append(errors, "missing required field: status")
	}
	if len(r.Tasks) == 0 {
		errors = append(errors, "missing required field: tasks (must have at least one task)")
	}

	for i, task := range r.Tasks {
		taskPrefix := "task[" + itoa(i) + "]"
		if task.ID == "" {
			errors = append(errors, taskPrefix+": missing required field: id")
		}
		if task.Description == "" {
			errors = append(errors, taskPrefix+": missing required field: description")
		}
		if len(task.AcceptanceCriteria) == 0 {
			errors = append(errors, taskPrefix+": missing required field: acceptance_criteria (must have at least one criterion)")
		}
	}

	if len(errors) > 0 {
		return &RefactorValidationError{Errors: errors}
	}
	return nil
}

// itoa converts an int to a string without importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// RefactorValidationError holds multiple validation errors for a refactor
type RefactorValidationError struct {
	Errors []string
}

func (e *RefactorValidationError) Error() string {
	return "refactor validation failed:\n  - " + joinStrings(e.Errors, "\n  - ")
}

// joinStrings joins strings with a separator without importing strings package
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
