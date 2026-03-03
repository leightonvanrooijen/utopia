package testutil

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestNewTestFeature(t *testing.T) {
	feature := NewTestFeature("login", "User can log in", "Email required", "Password required")

	if feature.ID != "login" {
		t.Errorf("expected ID 'login', got %q", feature.ID)
	}
	if feature.Description != "User can log in" {
		t.Errorf("expected Description 'User can log in', got %q", feature.Description)
	}
	if len(feature.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria, got %d", len(feature.AcceptanceCriteria))
	}
}

func TestNewTestFeature_NoCriteria(t *testing.T) {
	feature := NewTestFeature("simple", "Simple feature")

	if len(feature.AcceptanceCriteria) != 0 {
		t.Errorf("expected 0 criteria, got %d", len(feature.AcceptanceCriteria))
	}
}

func TestNewTestChangeRequest_Defaults(t *testing.T) {
	cr := NewTestChangeRequest("test-cr")

	if cr.ID != "test-cr" {
		t.Errorf("expected ID 'test-cr', got %q", cr.ID)
	}
	if cr.Type != domain.CRTypeFeature {
		t.Errorf("expected type Feature, got %q", cr.Type)
	}
	if cr.Title != "Test CR" {
		t.Errorf("expected title 'Test CR', got %q", cr.Title)
	}
	if cr.Status != domain.ChangeRequestDraft {
		t.Errorf("expected status Draft, got %q", cr.Status)
	}
}

func TestNewTestChangeRequest_WithOptions(t *testing.T) {
	cr := NewTestChangeRequest("auth-cr",
		WithTitle("Authentication Feature"),
		WithStatus(domain.ChangeRequestApproved),
		WithType(domain.CRTypeRefactor),
		WithParentSpec("auth-spec"),
	)

	if cr.Title != "Authentication Feature" {
		t.Errorf("expected title 'Authentication Feature', got %q", cr.Title)
	}
	if cr.Status != domain.ChangeRequestApproved {
		t.Errorf("expected status Approved, got %q", cr.Status)
	}
	if cr.Type != domain.CRTypeRefactor {
		t.Errorf("expected type Refactor, got %q", cr.Type)
	}
	if cr.ParentSpec != "auth-spec" {
		t.Errorf("expected ParentSpec 'auth-spec', got %q", cr.ParentSpec)
	}
}

func TestNewTestChangeRequest_WithAddFeatures(t *testing.T) {
	feature := NewTestFeature("login", "Login feature", "Works")
	cr := NewTestChangeRequest("feature-cr",
		WithAddFeatures("auth-spec", feature),
	)

	if len(cr.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(cr.Changes))
	}
	if cr.Changes[0].Operation != "add" {
		t.Errorf("expected operation 'add', got %q", cr.Changes[0].Operation)
	}
	if cr.Changes[0].Spec != "auth-spec" {
		t.Errorf("expected spec 'auth-spec', got %q", cr.Changes[0].Spec)
	}
	if cr.Changes[0].Feature.ID != "login" {
		t.Errorf("expected feature ID 'login', got %q", cr.Changes[0].Feature.ID)
	}
}

func TestNewTestWorkItem(t *testing.T) {
	item := NewTestWorkItem("work-1", "Implement the feature")

	if item.ID != "work-1" {
		t.Errorf("expected ID 'work-1', got %q", item.ID)
	}
	if item.Prompt != "Implement the feature" {
		t.Errorf("expected Prompt 'Implement the feature', got %q", item.Prompt)
	}
	if item.Status != domain.WorkItemPending {
		t.Errorf("expected status Pending, got %q", item.Status)
	}
	if item.Complexity != domain.ComplexityMedium {
		t.Errorf("expected complexity Medium, got %q", item.Complexity)
	}
}

func TestNewTestSpec_NoFeatures(t *testing.T) {
	spec := NewTestSpec("auth-spec")

	if spec.ID != "auth-spec" {
		t.Errorf("expected ID 'auth-spec', got %q", spec.ID)
	}
	if spec.Title != "Test Spec: auth-spec" {
		t.Errorf("expected title 'Test Spec: auth-spec', got %q", spec.Title)
	}
	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features, got %d", len(spec.Features))
	}
	if spec.Created.IsZero() {
		t.Error("expected Created to be set")
	}
	if spec.Updated.IsZero() {
		t.Error("expected Updated to be set")
	}
}

func TestNewTestSpec_WithFeatures(t *testing.T) {
	f1 := NewTestFeature("login", "Login", "Works")
	f2 := NewTestFeature("signup", "Signup", "Also works")
	spec := NewTestSpec("auth-spec", f1, f2)

	if len(spec.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(spec.Features))
	}
	if spec.Features[0].ID != "login" {
		t.Errorf("expected first feature ID 'login', got %q", spec.Features[0].ID)
	}
	if spec.Features[1].ID != "signup" {
		t.Errorf("expected second feature ID 'signup', got %q", spec.Features[1].ID)
	}
}
