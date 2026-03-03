package testutil

import (
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// CRWithFeatures creates a ChangeRequest with add operations for the given features.
// This is a test helper to reduce boilerplate when setting up test fixtures.
func CRWithFeatures(id string, features ...domain.Feature) *domain.ChangeRequest {
	cr := &domain.ChangeRequest{
		ID:    id,
		Type:  domain.CRTypeFeature,
		Title: "Test CR",
	}
	for _, f := range features {
		cr.Changes = append(cr.Changes, domain.Change{
			Operation: "add",
			Feature:   &f,
		})
	}
	return cr
}

// NewTestFeature creates a Feature with the given ID, description, and acceptance criteria.
// Reduces boilerplate for creating test fixtures.
func NewTestFeature(id, desc string, criteria ...string) domain.Feature {
	return domain.Feature{
		ID:                 id,
		Description:        desc,
		AcceptanceCriteria: criteria,
	}
}

// CROption is a functional option for configuring a ChangeRequest in tests.
type CROption func(*domain.ChangeRequest)

// WithTitle sets the change request title.
func WithTitle(title string) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Title = title
	}
}

// WithStatus sets the change request status.
func WithStatus(status domain.ChangeRequestStatus) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Status = status
	}
}

// WithType sets the change request type.
func WithType(crType domain.CRType) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Type = crType
	}
}

// WithParentSpec sets the parent spec ID.
func WithParentSpec(specID string) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.ParentSpec = specID
	}
}

// WithChanges sets the changes for feature/enhancement/removal CRs.
func WithChanges(changes ...domain.Change) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Changes = changes
	}
}

// WithTasks sets the tasks for refactor/bugfix CRs.
func WithTasks(tasks ...domain.Task) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Tasks = tasks
	}
}

// WithPhases sets the phases for initiative CRs.
func WithPhases(phases ...domain.Phase) CROption {
	return func(cr *domain.ChangeRequest) {
		cr.Phases = phases
	}
}

// WithAddFeatures is a convenience option that creates add operations for features.
func WithAddFeatures(specID string, features ...domain.Feature) CROption {
	return func(cr *domain.ChangeRequest) {
		for _, f := range features {
			f := f // capture loop variable
			cr.Changes = append(cr.Changes, domain.Change{
				Operation: "add",
				Spec:      specID,
				Feature:   &f,
			})
		}
	}
}

// NewTestChangeRequest creates a ChangeRequest with sensible test defaults.
// Use functional options to customize.
func NewTestChangeRequest(id string, opts ...CROption) *domain.ChangeRequest {
	cr := &domain.ChangeRequest{
		ID:     id,
		Type:   domain.CRTypeFeature,
		Title:  "Test CR",
		Status: domain.ChangeRequestDraft,
	}
	for _, opt := range opts {
		opt(cr)
	}
	return cr
}

// NewTestWorkItem creates a WorkItem with the given ID and prompt.
// Sets sensible defaults for other fields.
func NewTestWorkItem(id, prompt string) *domain.WorkItem {
	return &domain.WorkItem{
		ID:         id,
		Prompt:     prompt,
		Status:     domain.WorkItemPending,
		Complexity: domain.ComplexityMedium,
	}
}

// NewTestSpec creates a Spec with the given ID and optional features.
// Sets sensible defaults including timestamps.
func NewTestSpec(id string, features ...domain.Feature) *domain.Spec {
	now := time.Now()
	return &domain.Spec{
		ID:       id,
		Title:    "Test Spec: " + id,
		Created:  now,
		Updated:  now,
		Features: features,
	}
}
