package domain

import "fmt"

// NotFoundError indicates a resource could not be found.
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Is allows errors.Is to match any NotFoundError regardless of resource/id.
func (e *NotFoundError) Is(target error) bool {
	_, ok := target.(*NotFoundError)
	return ok
}

// ValidationError holds multiple validation errors.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("validation failed: %s", e.Errors[0])
	}
	result := "validation failed:"
	for _, err := range e.Errors {
		result += "\n  - " + err
	}
	return result
}

// Is allows errors.Is to match any ValidationError.
func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// ExecutionError captures context about a failure during work item execution.
type ExecutionError struct {
	Phase      string
	WorkItemID string
	Iteration  int
	Cause      error
}

func (e *ExecutionError) Error() string {
	if e.Iteration > 0 {
		return fmt.Sprintf("execution failed in phase %q for work item %s (iteration %d): %v",
			e.Phase, e.WorkItemID, e.Iteration, e.Cause)
	}
	return fmt.Sprintf("execution failed in phase %q for work item %s: %v",
		e.Phase, e.WorkItemID, e.Cause)
}

// Unwrap returns the underlying cause for errors.Unwrap.
func (e *ExecutionError) Unwrap() error {
	return e.Cause
}

// Is allows errors.Is to match any ExecutionError.
func (e *ExecutionError) Is(target error) bool {
	_, ok := target.(*ExecutionError)
	return ok
}
