package domain

import (
	"testing"
)

func TestRefactorStatus_Constants(t *testing.T) {
	tests := []struct {
		status   RefactorStatus
		expected string
	}{
		{RefactorStatusDraft, "draft"},
		{RefactorStatusReady, "ready"},
		{RefactorStatusInProgress, "in-progress"},
		{RefactorStatusComplete, "complete"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("got %q, want %q", tt.status, tt.expected)
			}
		})
	}
}

func TestNewRefactor(t *testing.T) {
	r := NewRefactor("extract-auth", "Extract authentication logic")

	if r.ID != "extract-auth" {
		t.Errorf("ID = %q, want %q", r.ID, "extract-auth")
	}

	if r.Title != "Extract authentication logic" {
		t.Errorf("Title = %q, want %q", r.Title, "Extract authentication logic")
	}

	if r.Status != RefactorStatusDraft {
		t.Errorf("Status = %q, want %q", r.Status, RefactorStatusDraft)
	}

	if len(r.Tasks) != 0 {
		t.Errorf("Tasks = %d, want 0", len(r.Tasks))
	}
}

func TestRefactor_AddTask(t *testing.T) {
	r := NewRefactor("test-refactor", "Test refactor")
	task := RefactorTask{
		ID:                 "task-1",
		Description:        "Extract interface",
		AcceptanceCriteria: []string{"Interface exists", "Tests pass"},
	}

	r.AddTask(task)

	if len(r.Tasks) != 1 {
		t.Fatalf("Tasks = %d, want 1", len(r.Tasks))
	}

	if r.Tasks[0].ID != "task-1" {
		t.Errorf("Task ID = %q, want %q", r.Tasks[0].ID, "task-1")
	}
}

func TestRefactor_MarkReady(t *testing.T) {
	t.Run("from draft succeeds", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		err := r.MarkReady()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Status != RefactorStatusReady {
			t.Errorf("Status = %q, want %q", r.Status, RefactorStatusReady)
		}
	})

	t.Run("from ready fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusReady
		err := r.MarkReady()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if _, ok := err.(*InvalidStatusTransitionError); !ok {
			t.Errorf("expected InvalidStatusTransitionError, got %T", err)
		}
	})

	t.Run("from in-progress fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusInProgress
		err := r.MarkReady()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("from complete fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusComplete
		err := r.MarkReady()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRefactor_MarkInProgress(t *testing.T) {
	t.Run("from ready succeeds", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusReady
		err := r.MarkInProgress()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Status != RefactorStatusInProgress {
			t.Errorf("Status = %q, want %q", r.Status, RefactorStatusInProgress)
		}
	})

	t.Run("from draft fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		err := r.MarkInProgress()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if _, ok := err.(*InvalidStatusTransitionError); !ok {
			t.Errorf("expected InvalidStatusTransitionError, got %T", err)
		}
	})

	t.Run("from in-progress fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusInProgress
		err := r.MarkInProgress()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("from complete fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusComplete
		err := r.MarkInProgress()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRefactor_MarkComplete(t *testing.T) {
	t.Run("from in-progress succeeds", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusInProgress
		err := r.MarkComplete()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Status != RefactorStatusComplete {
			t.Errorf("Status = %q, want %q", r.Status, RefactorStatusComplete)
		}
	})

	t.Run("from draft fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		err := r.MarkComplete()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if _, ok := err.(*InvalidStatusTransitionError); !ok {
			t.Errorf("expected InvalidStatusTransitionError, got %T", err)
		}
	})

	t.Run("from ready fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusReady
		err := r.MarkComplete()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("from complete fails", func(t *testing.T) {
		r := NewRefactor("test", "Test")
		r.Status = RefactorStatusComplete
		err := r.MarkComplete()

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRefactor_FullLifecycle(t *testing.T) {
	// Test the happy path: draft -> ready -> in-progress -> complete
	r := NewRefactor("extract-auth", "Extract authentication into separate package")

	// Add tasks while in draft
	r.AddTask(RefactorTask{
		ID:                 "create-package",
		Description:        "Create auth package structure",
		AcceptanceCriteria: []string{"Package exists at internal/auth"},
	})

	// Transition through lifecycle
	if err := r.MarkReady(); err != nil {
		t.Fatalf("MarkReady failed: %v", err)
	}

	if err := r.MarkInProgress(); err != nil {
		t.Fatalf("MarkInProgress failed: %v", err)
	}

	if err := r.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete failed: %v", err)
	}

	// Verify final state
	if r.Status != RefactorStatusComplete {
		t.Errorf("Status = %q, want %q", r.Status, RefactorStatusComplete)
	}
}

func TestInvalidStatusTransitionError_Error(t *testing.T) {
	err := &InvalidStatusTransitionError{
		From: RefactorStatusDraft,
		To:   RefactorStatusComplete,
	}

	expected := "invalid status transition from draft to complete"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
