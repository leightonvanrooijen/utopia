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
