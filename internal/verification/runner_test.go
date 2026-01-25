package verification

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("/tmp")

	if r == nil {
		t.Fatal("NewRunner returned nil")
	}

	if r.workDir != "/tmp" {
		t.Errorf("workDir = %q, want %q", r.workDir, "/tmp")
	}
}

func TestRunner_Run_PassingCommand(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// "true" always exits with 0
	result, err := r.Run(ctx, "true")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.Passed {
		t.Error("expected Passed=true for 'true' command")
	}
}

func TestRunner_Run_FailingCommand(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// "false" always exits with 1
	result, err := r.Run(ctx, "false")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for 'false' command")
	}
}

func TestRunner_Run_CapturesOutput(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	result, err := r.Run(ctx, "echo 'hello world'")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result.Output, "hello world") {
		t.Errorf("Output = %q, should contain 'hello world'", result.Output)
	}
}

func TestRunner_Run_CapturesStderr(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// Write to stderr
	result, err := r.Run(ctx, "echo 'error message' >&2")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result.Output, "error message") {
		t.Errorf("Output = %q, should contain stderr 'error message'", result.Output)
	}
}

func TestRunner_Run_CombinesStdoutStderr(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// Write to both stdout and stderr
	result, err := r.Run(ctx, "echo 'stdout'; echo 'stderr' >&2")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result.Output, "stdout") {
		t.Errorf("Output should contain 'stdout', got %q", result.Output)
	}

	if !strings.Contains(result.Output, "stderr") {
		t.Errorf("Output should contain 'stderr', got %q", result.Output)
	}
}

func TestRunner_Run_EmptyCommand(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	_, err := r.Run(ctx, "")

	if err == nil {
		t.Error("expected error for empty command")
	}

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got %q", err.Error())
	}
}

func TestRunner_Run_NonZeroExitCode(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// Exit with specific code
	result, err := r.Run(ctx, "exit 42")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for non-zero exit")
	}
}

func TestRunner_Run_ContextCancellation(t *testing.T) {
	r := NewRunner("/tmp")

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Long-running command
	result, err := r.Run(ctx, "sleep 10")

	// Run() doesn't return an error for command failures,
	// it returns Passed=false instead
	if err != nil {
		t.Fatalf("Run failed unexpectedly: %v", err)
	}

	// Command should have failed due to cancelled context
	if result.Passed {
		t.Error("expected Passed=false due to cancelled context")
	}
}

func TestRunner_Run_Timeout(t *testing.T) {
	r := NewRunner("/tmp")

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Command that takes longer than timeout
	result, err := r.Run(ctx, "sleep 5")

	// Run should not return an error (context error is handled internally)
	// but the command should have failed
	if err != nil {
		// Context deadline exceeded is acceptable
		return
	}

	if result.Passed {
		t.Error("expected Passed=false for timed-out command")
	}
}

func TestRunner_Run_WorkingDirectory(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// pwd should return the working directory
	result, err := r.Run(ctx, "pwd")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result.Output, "/tmp") {
		t.Errorf("pwd should be /tmp, got %q", result.Output)
	}
}

func TestTruncateTail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world",
			maxLen:   5,
			expected: "world", // keeps tail
		},
		{
			name:     "keeps last N chars",
			input:    "abcdefghij",
			maxLen:   3,
			expected: "hij",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "maxLen of 0",
			input:    "hello",
			maxLen:   0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTail(tt.input, tt.maxLen)

			if got != tt.expected {
				t.Errorf("truncateTail(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestMaxOutputLength(t *testing.T) {
	// Verify the constant is set to a reasonable value
	if MaxOutputLength <= 0 {
		t.Errorf("MaxOutputLength should be positive, got %d", MaxOutputLength)
	}

	if MaxOutputLength < 1000 {
		t.Errorf("MaxOutputLength seems too small: %d", MaxOutputLength)
	}
}

func TestResult_Fields(t *testing.T) {
	result := &Result{
		Passed: true,
		Output: "All tests passed",
	}

	if !result.Passed {
		t.Error("Passed should be true")
	}

	if result.Output != "All tests passed" {
		t.Errorf("Output = %q, want %q", result.Output, "All tests passed")
	}
}

func TestRunner_Run_ShellCommand(t *testing.T) {
	r := NewRunner("/tmp")
	ctx := context.Background()

	// Test that complex shell commands work (pipes, etc.)
	result, err := r.Run(ctx, "echo 'line1\nline2\nline3' | wc -l")

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.Passed {
		t.Error("expected command to pass")
	}

	// Should contain "3" (the line count)
	if !strings.Contains(result.Output, "3") {
		t.Errorf("Output should contain line count '3', got %q", result.Output)
	}
}
