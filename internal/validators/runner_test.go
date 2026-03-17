package validators

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
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
