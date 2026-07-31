package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// requeueProject writes a work item into a bare .utopia tree and returns the
// project directory. Nothing here needs a full init: requeue reads and writes
// one work item file.
func requeueProject(t *testing.T, specID string, item *domain.WorkItem) string {
	t.Helper()
	projectDir := t.TempDir()
	utopiaDir := filepath.Join(projectDir, ".utopia")
	if err := os.MkdirAll(filepath.Join(utopiaDir, "work-items", specID), 0755); err != nil {
		t.Fatalf("failed to create work-items dir: %v", err)
	}
	store := internal.NewYAMLStore(utopiaDir)
	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		t.Fatalf("failed to save work item: %v", err)
	}
	return projectDir
}

func runRequeueCmd(t *testing.T, projectDir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRequeueCmd()
	cmd.Flags().String("project", projectDir, "")
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRequeueResetsAHaltedItemOnDisk(t *testing.T) {
	specID := "auth"
	projectDir := requeueProject(t, specID, &domain.WorkItem{
		ID:                        "auth-signup",
		SpecRef:                   "auth.signup",
		Status:                    domain.WorkItemNeedsHuman,
		IterationCount:            7,
		ComprehensionCount:        2,
		OpusExecutionAttempts:     3,
		ComprehensionFailureTotal: 4,
		InvocationErrorCount:      2,
		LastValidatorFeedback:     "the handler never validates the token",
		FailureConclusions:        []domain.FailureConclusion{{Iteration: 6, ValidatorID: "spec-intent", Diagnosis: "misread"}},
		ExecutorAttempts:          []domain.ExecutorAttempt{{Iteration: 6, Role: domain.ExecutorRoleEscalated, Model: "opus"}},
	})

	stdout, _, err := runRequeueCmd(t, projectDir, "auth-signup")
	if err != nil {
		t.Fatalf("requeue error = %v, want nil", err)
	}

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	saved, _, err := store.FindWorkItem("auth-signup")
	if err != nil {
		t.Fatalf("failed to reload work item: %v", err)
	}
	if saved.Status != domain.WorkItemPending {
		t.Errorf("persisted status = %q, want %q", saved.Status, domain.WorkItemPending)
	}
	if saved.IterationCount != 0 || saved.ComprehensionCount != 0 || saved.OpusExecutionAttempts != 0 ||
		saved.ComprehensionFailureTotal != 0 || saved.InvocationErrorCount != 0 {
		t.Errorf("persisted counters not cleared: %+v", saved)
	}
	if saved.LastValidatorFeedback != "" || saved.FailureConclusions != nil {
		t.Errorf("persisted diagnosis not cleared: feedback %q, conclusions %v",
			saved.LastValidatorFeedback, saved.FailureConclusions)
	}
	if len(saved.ExecutorAttempts) != 1 {
		t.Errorf("persisted executor attempts = %v, want the recorded spend kept", saved.ExecutorAttempts)
	}

	// The operator has to be able to see the item was reset, not merely unblocked.
	for _, want := range []string{"needs_human -> pending", "iteration_count (was 7)", "opus_execution_attempts (was 3)", "failure_conclusions"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q, got:\n%s", want, stdout)
		}
	}
}

func TestRequeueFindsAnItemInAPhaseDirectory(t *testing.T) {
	projectDir := requeueProject(t, filepath.Join("platform", "phase-2"), &domain.WorkItem{
		ID:             "platform-phase-2-api",
		Status:         domain.WorkItemNeedsHuman,
		IterationCount: 3,
	})

	if _, _, err := runRequeueCmd(t, projectDir, "platform-phase-2-api"); err != nil {
		t.Fatalf("requeue error = %v, want nil", err)
	}

	path := filepath.Join(projectDir, ".utopia", "work-items", "platform", "phase-2", "platform-phase-2-api.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if !strings.Contains(string(data), "status: pending") {
		t.Errorf("phase-scoped item not requeued in place, file:\n%s", data)
	}
}

func TestRequeueRefusesAnItemThatIsNotHalted(t *testing.T) {
	projectDir := requeueProject(t, "auth", &domain.WorkItem{
		ID:             "auth-signup",
		Status:         domain.WorkItemCompleted,
		IterationCount: 3,
	})

	_, _, err := runRequeueCmd(t, projectDir, "auth-signup")
	if err == nil {
		t.Fatal("requeue of a completed item returned nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "not halted") || !strings.Contains(err.Error(), "completed") {
		t.Errorf("error = %q, want it to name the refusal and the status found", err)
	}

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	saved, _, loadErr := store.FindWorkItem("auth-signup")
	if loadErr != nil {
		t.Fatalf("failed to reload work item: %v", loadErr)
	}
	if saved.Status != domain.WorkItemCompleted || saved.IterationCount != 3 {
		t.Errorf("refused requeue rewrote the item: status %q, iteration_count %d", saved.Status, saved.IterationCount)
	}
}

func TestRequeueUnknownItem(t *testing.T) {
	projectDir := requeueProject(t, "auth", &domain.WorkItem{ID: "auth-signup", Status: domain.WorkItemNeedsHuman})

	_, _, err := runRequeueCmd(t, projectDir, "auth-login")
	if err == nil {
		t.Fatal("requeue of an unknown item returned nil, want an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want a not-found message", err)
	}
}
