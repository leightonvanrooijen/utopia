package domain

import (
	"testing"
)

func TestWorkItemStatus_Constants(t *testing.T) {
	// Verify status constants have expected string values
	tests := []struct {
		status   WorkItemStatus
		expected string
	}{
		{WorkItemPending, "pending"},
		{WorkItemInProgress, "in_progress"},
		{WorkItemCompleted, "completed"},
		{WorkItemFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("got %q, want %q", tt.status, tt.expected)
			}
		})
	}
}

func TestNewWorkItem(t *testing.T) {
	feature := Feature{
		ID:                 "signup",
		Description:        "User signup feature",
		AcceptanceCriteria: []string{"Users can register", "Email validation works"},
	}

	item := NewWorkItem("test-item", "auth-spec", "signup", feature, 5)

	// Verify basic fields
	if item.ID != "test-item" {
		t.Errorf("ID = %q, want %q", item.ID, "test-item")
	}

	if item.SpecRef != "auth-spec.signup" {
		t.Errorf("SpecRef = %q, want %q", item.SpecRef, "auth-spec.signup")
	}

	if item.Title != feature.Description {
		t.Errorf("Title = %q, want %q", item.Title, feature.Description)
	}

	if item.Status != WorkItemPending {
		t.Errorf("Status = %q, want %q", item.Status, WorkItemPending)
	}

	if item.Order != 5 {
		t.Errorf("Order = %d, want %d", item.Order, 5)
	}

	if item.Complexity != ComplexityMedium {
		t.Errorf("Complexity = %q, want %q", item.Complexity, ComplexityMedium)
	}

	// Verify new fields default to zero values
	if item.IterationCount != 0 {
		t.Errorf("IterationCount = %d, want %d", item.IterationCount, 0)
	}

	if item.LastFailureOutput != "" {
		t.Errorf("LastFailureOutput = %q, want empty string", item.LastFailureOutput)
	}
}

func TestWorkItem_IterationTracking(t *testing.T) {
	item := &WorkItem{
		ID:     "test-item",
		Status: WorkItemPending,
	}

	// Simulate execution iterations
	item.Status = WorkItemInProgress
	item.IterationCount = 1

	if item.IterationCount != 1 {
		t.Errorf("IterationCount = %d, want %d", item.IterationCount, 1)
	}

	// Simulate failure with output
	item.IterationCount = 2
	item.LastFailureOutput = "test failed: expected 1 got 2"

	if item.LastFailureOutput != "test failed: expected 1 got 2" {
		t.Errorf("LastFailureOutput not set correctly")
	}

	// Simulate success - clear failure output
	item.IterationCount = 3
	item.Status = WorkItemCompleted
	item.LastFailureOutput = ""

	if item.Status != WorkItemCompleted {
		t.Errorf("Status = %q, want %q", item.Status, WorkItemCompleted)
	}

	if item.LastFailureOutput != "" {
		t.Errorf("LastFailureOutput should be cleared on completion")
	}
}

func TestComplexity_Constants(t *testing.T) {
	tests := []struct {
		complexity Complexity
		expected   string
	}{
		{ComplexityLow, "low"},
		{ComplexityMedium, "medium"},
		{ComplexityHigh, "high"},
	}

	for _, tt := range tests {
		t.Run(string(tt.complexity), func(t *testing.T) {
			if string(tt.complexity) != tt.expected {
				t.Errorf("got %q, want %q", tt.complexity, tt.expected)
			}
		})
	}
}
