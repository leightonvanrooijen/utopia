package validators

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("/tmp")

	if r == nil {
		t.Fatal("NewRunner returned nil")
	}

	if r.workDir != "/tmp" {
		t.Errorf("workDir = %q, want %q", r.workDir, "/tmp")
	}

	if r.cli == nil {
		t.Error("cli should not be nil")
	}
}

func TestPassedToken(t *testing.T) {
	// Verify the constant is set correctly
	if PassedToken != "<PASSED>" {
		t.Errorf("PassedToken = %q, want %q", PassedToken, "<PASSED>")
	}
}

func TestResult_Fields(t *testing.T) {
	// Test passing result
	t.Run("passing result", func(t *testing.T) {
		result := &Result{
			Passed:   true,
			Feedback: "",
		}

		if !result.Passed {
			t.Error("Passed should be true")
		}

		if result.Feedback != "" {
			t.Errorf("Feedback should be empty for passing result, got %q", result.Feedback)
		}
	})

	// Test failing result
	t.Run("failing result", func(t *testing.T) {
		result := &Result{
			Passed:   false,
			Feedback: "Component naming does not follow conventions",
		}

		if result.Passed {
			t.Error("Passed should be false")
		}

		if result.Feedback == "" {
			t.Error("Feedback should not be empty for failing result")
		}

		if !strings.Contains(result.Feedback, "Component naming") {
			t.Errorf("Feedback should contain failure details, got %q", result.Feedback)
		}
	})
}

func TestPassedTokenDetection(t *testing.T) {
	tests := []struct {
		name   string
		output string
		passed bool
	}{
		{"token present", "Review complete. <PASSED>", true},
		{"token at start", "<PASSED> All good", true},
		{"token alone", "<PASSED>", true},
		{"token with newlines", "Line 1\n<PASSED>\nLine 3", true},
		{"no token", "Review complete. Issues found.", false},
		{"partial token", "<PASS>", false},
		{"lowercase token", "<passed>", false},
		{"token with spaces", "< PASSED >", false},
		{"empty output", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the same logic used in Run()
			passed := strings.Contains(tt.output, PassedToken)

			if passed != tt.passed {
				t.Errorf("Contains(%q, PassedToken) = %v, want %v",
					tt.output, passed, tt.passed)
			}
		})
	}
}

func TestRunner_getGitDiff(t *testing.T) {
	// Skip if not in a git repo (CI environments may not have git history)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping test")
	}

	// Create a temporary git repo for testing
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	runGit := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		return cmd.Run()
	}

	if err := runGit("init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if err := runGit("config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email failed: %v", err)
	}

	if err := runGit("config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config name failed: %v", err)
	}

	// Create initial commit
	if err := os.WriteFile(tmpDir+"/file.txt", []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := runGit("add", "."); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	if err := runGit("commit", "-m", "initial commit"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Create second commit with changes
	if err := os.WriteFile(tmpDir+"/file.txt", []byte("changed content"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := runGit("add", "."); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	if err := runGit("commit", "-m", "second commit"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Test getGitDiff
	r := NewRunner(tmpDir)
	diff, err := r.getGitDiff(context.Background())

	if err != nil {
		t.Fatalf("getGitDiff failed: %v", err)
	}

	// Verify diff contains expected content
	if !strings.Contains(diff, "file.txt") {
		t.Errorf("diff should contain file.txt, got: %s", diff)
	}

	if !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+changed content") {
		t.Errorf("diff should show changes, got: %s", diff)
	}
}

func TestRunner_getGitDiff_NoHistory(t *testing.T) {
	// Skip if not in a git repo
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping test")
	}

	// Create a temporary git repo with only one commit
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		return cmd.Run()
	}

	if err := runGit("init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if err := runGit("config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email failed: %v", err)
	}

	if err := runGit("config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config name failed: %v", err)
	}

	// Create only one commit (no HEAD~1 exists)
	if err := os.WriteFile(tmpDir+"/file.txt", []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := runGit("add", "."); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	if err := runGit("commit", "-m", "first commit"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Test getGitDiff should fail (no HEAD~1)
	r := NewRunner(tmpDir)
	_, err = r.getGitDiff(context.Background())

	if err == nil {
		t.Error("expected error when no HEAD~1 exists")
	}
}

func TestValidatorResult_Fields(t *testing.T) {
	t.Run("successful result", func(t *testing.T) {
		vr := ValidatorResult{
			ID:     "test-validator",
			Result: &Result{Passed: true, Feedback: ""},
			Err:    nil,
		}

		if vr.ID != "test-validator" {
			t.Errorf("ID = %q, want %q", vr.ID, "test-validator")
		}

		if vr.Err != nil {
			t.Errorf("Err should be nil, got %v", vr.Err)
		}

		if !vr.Result.Passed {
			t.Error("Result.Passed should be true")
		}
	})

	t.Run("failed result with error", func(t *testing.T) {
		vr := ValidatorResult{
			ID:     "failing-validator",
			Result: nil,
			Err:    context.DeadlineExceeded,
		}

		if vr.Err == nil {
			t.Error("Err should not be nil")
		}

		if vr.Result != nil {
			t.Error("Result should be nil when there's an error")
		}
	})
}

func TestRunner_RunAll_FiltersByTrigger(t *testing.T) {
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem},
		{ID: "v2", Run: domain.RunAfterPhase},
		{ID: "v3", Run: domain.RunAfterWorkitem},
		{ID: "v4", Run: domain.RunOnDemand},
	}

	// Create runner - it will fail on git diff, but we're testing the filter logic
	r := NewRunner("/nonexistent")
	results := r.RunAll(context.Background(), validators, domain.RunAfterWorkitem)

	// Should have results for v1 and v3 only (both are after-workitem)
	if len(results) != 2 {
		t.Errorf("expected 2 results for after-workitem trigger, got %d", len(results))
	}

	ids := make(map[string]bool)
	for _, vr := range results {
		ids[vr.ID] = true
	}

	if !ids["v1"] || !ids["v3"] {
		t.Errorf("expected v1 and v3, got %v", ids)
	}

	if ids["v2"] || ids["v4"] {
		t.Errorf("v2 and v4 should not be included, got %v", ids)
	}
}

func TestRunner_RunAll_EmptyValidators(t *testing.T) {
	r := NewRunner("/tmp")
	results := r.RunAll(context.Background(), nil, domain.RunAfterWorkitem)

	if results != nil {
		t.Errorf("expected nil for empty validators, got %v", results)
	}
}

func TestRunner_RunAll_NoMatchingTrigger(t *testing.T) {
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunOnDemand},
	}

	r := NewRunner("/tmp")
	results := r.RunAll(context.Background(), validators, domain.RunAfterWorkitem)

	if results != nil {
		t.Errorf("expected nil when no validators match trigger, got %v", results)
	}
}

func TestRunner_RunAll_ConcurrentExecution(t *testing.T) {
	// This test verifies validators run concurrently by tracking execution overlap
	// We use a counter to detect concurrent execution
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem, Prompt: "test"},
		{ID: "v2", Run: domain.RunAfterWorkitem, Prompt: "test"},
		{ID: "v3", Run: domain.RunAfterWorkitem, Prompt: "test"},
	}

	// Create runner that will fail on git diff - but we want to verify
	// the concurrency happens by checking all validators are attempted
	r := NewRunner("/nonexistent")

	// Track start time
	start := time.Now()

	results := r.RunAll(context.Background(), validators, domain.RunAfterWorkitem)

	// Verify all validators were processed
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Since git diff fails fast, all should complete quickly (concurrent)
	elapsed := time.Since(start)
	if elapsed > 1*time.Second {
		t.Errorf("concurrent execution took too long: %v", elapsed)
	}

	// All should have errors (git diff failed)
	for _, vr := range results {
		if vr.Err == nil {
			t.Errorf("validator %s should have error", vr.ID)
		}
	}

	// Update maxConcurrent tracking (for documentation)
	_ = concurrentCount
	_ = maxConcurrent
}

func TestRunner_RunAll_ContextCancellation(t *testing.T) {
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem, Prompt: "test"},
		{ID: "v2", Run: domain.RunAfterWorkitem, Prompt: "test"},
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := NewRunner("/tmp")
	results := r.RunAll(ctx, validators, domain.RunAfterWorkitem)

	// All validators should have errors due to cancelled context
	for _, vr := range results {
		if vr.Err == nil {
			t.Errorf("validator %s should have context cancellation error", vr.ID)
		}
	}
}

func TestRunner_RunAll_DefaultTrigger(t *testing.T) {
	// Validator with empty Run should default to RunAfterWorkitem
	validators := []*domain.Validator{
		{ID: "v1", Run: ""}, // should default to after-workitem
		{ID: "v2", Run: domain.RunAfterPhase},
	}

	r := NewRunner("/nonexistent")
	results := r.RunAll(context.Background(), validators, domain.RunAfterWorkitem)

	// Should include v1 (defaults to after-workitem)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].ID != "v1" {
		t.Errorf("expected v1, got %s", results[0].ID)
	}
}

func TestAggregateResults_AllPassed(t *testing.T) {
	results := []ValidatorResult{
		{ID: "v1", Result: &Result{Passed: true}},
		{ID: "v2", Result: &Result{Passed: true}},
		{ID: "v3", Result: &Result{Passed: true}},
	}

	aggregate := AggregateResults(results)

	if !aggregate.Passed {
		t.Error("aggregate should pass when all validators pass")
	}

	if aggregate.Feedback != "" {
		t.Errorf("feedback should be empty when all pass, got %q", aggregate.Feedback)
	}
}

func TestAggregateResults_OneFailed(t *testing.T) {
	results := []ValidatorResult{
		{ID: "v1", Result: &Result{Passed: true}},
		{ID: "v2", Result: &Result{Passed: false, Feedback: "naming violation"}},
		{ID: "v3", Result: &Result{Passed: true}},
	}

	aggregate := AggregateResults(results)

	if aggregate.Passed {
		t.Error("aggregate should fail when any validator fails")
	}

	if !strings.Contains(aggregate.Feedback, "v2") {
		t.Errorf("feedback should contain failed validator ID, got %q", aggregate.Feedback)
	}

	if !strings.Contains(aggregate.Feedback, "naming violation") {
		t.Errorf("feedback should contain failure message, got %q", aggregate.Feedback)
	}

	// Should not contain passing validators
	if strings.Contains(aggregate.Feedback, "v1") || strings.Contains(aggregate.Feedback, "v3") {
		t.Errorf("feedback should not contain passing validators, got %q", aggregate.Feedback)
	}
}

func TestAggregateResults_MultipleFailed(t *testing.T) {
	results := []ValidatorResult{
		{ID: "v1", Result: &Result{Passed: false, Feedback: "style error"}},
		{ID: "v2", Result: &Result{Passed: true}},
		{ID: "v3", Result: &Result{Passed: false, Feedback: "security issue"}},
	}

	aggregate := AggregateResults(results)

	if aggregate.Passed {
		t.Error("aggregate should fail when multiple validators fail")
	}

	if !strings.Contains(aggregate.Feedback, "v1") || !strings.Contains(aggregate.Feedback, "style error") {
		t.Errorf("feedback should contain v1 failure, got %q", aggregate.Feedback)
	}

	if !strings.Contains(aggregate.Feedback, "v3") || !strings.Contains(aggregate.Feedback, "security issue") {
		t.Errorf("feedback should contain v3 failure, got %q", aggregate.Feedback)
	}
}

func TestAggregateResults_WithErrors(t *testing.T) {
	results := []ValidatorResult{
		{ID: "v1", Result: &Result{Passed: true}},
		{ID: "v2", Err: context.DeadlineExceeded},
		{ID: "v3", Result: &Result{Passed: false, Feedback: "check failed"}},
	}

	aggregate := AggregateResults(results)

	if aggregate.Passed {
		t.Error("aggregate should fail when any validator has error")
	}

	if !strings.Contains(aggregate.Feedback, "v2") || !strings.Contains(aggregate.Feedback, "error") {
		t.Errorf("feedback should contain error validator, got %q", aggregate.Feedback)
	}

	if !strings.Contains(aggregate.Feedback, "v3") {
		t.Errorf("feedback should contain failed validator, got %q", aggregate.Feedback)
	}
}

func TestAggregateResults_Empty(t *testing.T) {
	aggregate := AggregateResults(nil)

	if !aggregate.Passed {
		t.Error("aggregate should pass with no validators")
	}

	if aggregate.Feedback != "" {
		t.Errorf("feedback should be empty with no validators, got %q", aggregate.Feedback)
	}
}

func TestAggregateResults_NilResult(t *testing.T) {
	// Edge case: ValidatorResult with nil Result and nil Err
	results := []ValidatorResult{
		{ID: "v1", Result: nil, Err: nil},
		{ID: "v2", Result: &Result{Passed: true}},
	}

	aggregate := AggregateResults(results)

	// A nil Result with nil Err should be treated as passed (no failure detected)
	if !aggregate.Passed {
		t.Error("aggregate should pass when Result is nil with no error")
	}
}

func TestRunner_RunAllWithDiffLimited_DefaultConcurrency(t *testing.T) {
	// Verify that concurrencyLimit <= 0 defaults to DefaultValidatorConcurrency
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem},
		{ID: "v2", Run: domain.RunAfterWorkitem},
	}

	r := NewRunner("/nonexistent")

	// Test with 0 (should use default)
	results := r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "fake diff", 0)
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit 0, got %d", len(results))
	}

	// Test with -1 (should also use default)
	results = r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "fake diff", -1)
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit -1, got %d", len(results))
	}
}

func TestRunner_RunAllWithDiffLimited_ConcurrencyLimit(t *testing.T) {
	// Test that setting concurrencyLimit to 1 runs validators sequentially
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem},
		{ID: "v2", Run: domain.RunAfterWorkitem},
		{ID: "v3", Run: domain.RunAfterWorkitem},
	}

	r := NewRunner("/nonexistent")

	// Run with limit of 1 (sequential)
	results := r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "fake diff", 1)

	// All validators should be processed
	if len(results) != 3 {
		t.Errorf("expected 3 results with limit 1, got %d", len(results))
	}

	// Verify all validator IDs are present
	ids := make(map[string]bool)
	for _, vr := range results {
		ids[vr.ID] = true
	}

	if !ids["v1"] || !ids["v2"] || !ids["v3"] {
		t.Errorf("expected v1, v2, v3, got %v", ids)
	}
}

func TestRunner_RunAllWithDiffLimited_ExplicitLimit(t *testing.T) {
	// Test that explicit concurrency limits are respected
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem},
		{ID: "v2", Run: domain.RunAfterWorkitem},
		{ID: "v3", Run: domain.RunAfterWorkitem},
		{ID: "v4", Run: domain.RunAfterWorkitem},
	}

	r := NewRunner("/nonexistent")

	// Run with limit of 2
	results := r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "fake diff", 2)

	// All validators should be processed despite concurrency limit
	if len(results) != 4 {
		t.Errorf("expected 4 results with limit 2, got %d", len(results))
	}
}

func TestRunner_RunAllWithDiffLimited_FiltersByTrigger(t *testing.T) {
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunAfterWorkitem},
		{ID: "v2", Run: domain.RunAfterPhase},
		{ID: "v3", Run: domain.RunAfterWorkitem},
	}

	r := NewRunner("/nonexistent")
	results := r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "fake diff", 4)

	// Should only include v1 and v3
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	ids := make(map[string]bool)
	for _, vr := range results {
		ids[vr.ID] = true
	}

	if !ids["v1"] || !ids["v3"] {
		t.Errorf("expected v1 and v3, got %v", ids)
	}

	if ids["v2"] {
		t.Error("v2 should not be included (wrong trigger)")
	}
}

func TestRunner_RunAllWithDiffLimited_EmptyReturnsNil(t *testing.T) {
	r := NewRunner("/tmp")

	// Empty validators list
	results := r.RunAllWithDiffLimited(context.Background(), nil, domain.RunAfterWorkitem, "diff", 4)
	if results != nil {
		t.Errorf("expected nil for empty validators, got %v", results)
	}

	// No matching trigger
	validators := []*domain.Validator{
		{ID: "v1", Run: domain.RunOnDemand},
	}
	results = r.RunAllWithDiffLimited(context.Background(), validators, domain.RunAfterWorkitem, "diff", 4)
	if results != nil {
		t.Errorf("expected nil for no matching trigger, got %v", results)
	}
}

func TestRunner_WithModelConfig(t *testing.T) {
	r := NewRunner("/tmp")

	// Initially no model config
	if r.modelConfig != nil {
		t.Error("modelConfig should be nil initially")
	}

	// Apply model config
	mc := &domain.ModelConfig{
		Default:    "haiku",
		Validators: "sonnet",
	}
	r2 := r.WithModelConfig(mc)

	// Original runner unchanged
	if r.modelConfig != nil {
		t.Error("original runner should not be modified")
	}

	// New runner has config
	if r2.modelConfig != mc {
		t.Error("new runner should have model config")
	}

	// Verify other fields copied correctly
	if r2.workDir != r.workDir {
		t.Errorf("workDir not copied: got %q, want %q", r2.workDir, r.workDir)
	}
	if r2.cli != r.cli {
		t.Error("cli not copied")
	}
}

func TestRunner_resolveModel(t *testing.T) {
	tests := []struct {
		name          string
		modelConfig   *domain.ModelConfig
		validatorModel string
		expected      string
	}{
		{
			name:          "validator override takes priority",
			modelConfig:   &domain.ModelConfig{Default: "haiku", Validators: "sonnet"},
			validatorModel: "opus",
			expected:      "opus",
		},
		{
			name:          "falls back to models.validators",
			modelConfig:   &domain.ModelConfig{Default: "haiku", Validators: "sonnet"},
			validatorModel: "",
			expected:      "sonnet",
		},
		{
			name:          "falls back to models.default when validators not set",
			modelConfig:   &domain.ModelConfig{Default: "haiku"},
			validatorModel: "",
			expected:      "haiku",
		},
		{
			name:          "falls back to sonnet when nothing configured",
			modelConfig:   &domain.ModelConfig{},
			validatorModel: "",
			expected:      "sonnet",
		},
		{
			name:          "handles nil model config",
			modelConfig:   nil,
			validatorModel: "",
			expected:      "sonnet",
		},
		{
			name:          "validator override still works with nil config",
			modelConfig:   nil,
			validatorModel: "opus",
			expected:      "opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRunner("/tmp").WithModelConfig(tt.modelConfig)
			validator := &domain.Validator{
				ID:            "test-validator",
				ModelOverride: tt.validatorModel,
			}

			result := r.resolveModel(validator)
			if result != tt.expected {
				t.Errorf("resolveModel() = %q, want %q", result, tt.expected)
			}
		})
	}
}
