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
