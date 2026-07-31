package validators

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// A validator whose invocation failed has not disapproved of anything, so the
// runner invokes it again rather than reporting a fault as a verdict. These tests
// drive the runner against a stand-in claude binary that fails a chosen number of
// times, because the retry is about what the runner does with the invocation
// rather than about how an error is later classified.

func TestRunner_RetriesFailedInvocation(t *testing.T) {
	countPath := flakyClaudeOnPath(t, 2)

	results := NewRunner(t.TempDir()).RunAllWithDiffLimited(
		context.Background(),
		[]*domain.Validator{{ID: "v1", Run: domain.RunAfterWorkitem}},
		domain.RunAfterWorkitem, "diff", 1)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("expected the retried invocation to succeed, got %v", results[0].Err)
	}
	if results[0].Result == nil || !results[0].Result.Passed {
		t.Errorf("expected a passing result, got %+v", results[0].Result)
	}
	if got := invocations(t, countPath); got != 3 {
		t.Errorf("claude was invoked %d time(s), want 3 (the attempt plus %d retries)", got, DefaultValidatorInvocationRetries)
	}
}

func TestRunner_StopsAtTheRetryCap(t *testing.T) {
	countPath := flakyClaudeOnPath(t, 99)

	results := NewRunner(t.TempDir()).RunAllWithDiffLimited(
		context.Background(),
		[]*domain.Validator{{ID: "v1", Run: domain.RunAfterWorkitem}},
		domain.RunAfterWorkitem, "diff", 1)

	if results[0].Err == nil {
		t.Fatal("expected the exhausted retries to be reported as an error")
	}
	if !strings.Contains(results[0].Err.Error(), "3 attempt(s)") {
		t.Errorf("error = %q, want it to report how many attempts were made", results[0].Err)
	}
	if got := invocations(t, countPath); got != 3 {
		t.Errorf("claude was invoked %d time(s), want 3", got)
	}

	// The unresolved run is carried as errored, not as a comprehension failure.
	aggregate := AggregateResults(results)
	if aggregate.Passed {
		t.Error("aggregate should not pass when a validator never ran")
	}
	if aggregate.FailureClass != "" {
		t.Errorf("FailureClass = %q, want empty", aggregate.FailureClass)
	}
	if len(aggregate.Errors) != 1 {
		t.Errorf("expected 1 errored validator, got %d", len(aggregate.Errors))
	}
}

func TestRunner_InvocationRetriesAreConfigurable(t *testing.T) {
	tests := map[string]struct {
		retries int
		want    int
	}{
		"no retries invokes once":   {retries: 0, want: 1},
		"four retries invoke five":  {retries: 4, want: 5},
		"a negative value defaults": {retries: -1, want: 1 + DefaultValidatorInvocationRetries},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			countPath := flakyClaudeOnPath(t, 99)

			r := NewRunner(t.TempDir()).WithInvocationRetries(tt.retries)
			r.RunAllWithDiffLimited(context.Background(),
				[]*domain.Validator{{ID: "v1", Run: domain.RunAfterWorkitem}},
				domain.RunAfterWorkitem, "diff", 1)

			if got := invocations(t, countPath); got != tt.want {
				t.Errorf("claude was invoked %d time(s), want %d", got, tt.want)
			}
		})
	}
}

// flakyClaudeOnPath puts a stand-in claude on PATH that exits non-zero for its
// first n invocations and then reports a passing verdict. It returns the path of
// the file the script appends to once per invocation.
func flakyClaudeOnPath(t *testing.T, failures int) string {
	t.Helper()

	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	script := "#!/bin/sh\n" +
		"printf 'x' >> " + countPath + "\n" +
		"if [ $(wc -c < " + countPath + ") -le " + strconv.Itoa(failures) + " ]; then\n" +
		`  echo 'claude: connection reset' >&2` + "\n" +
		"  exit 1\n" +
		"fi\n" +
		// Validators run without usage capture, so claude answers in plain text
		// rather than in a JSON envelope.
		`echo '<VERDICT>{"verdict":"pass"}</VERDICT>'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return countPath
}

// invocations reports how many times the stand-in claude ran.
func invocations(t *testing.T, countPath string) int {
	t.Helper()

	data, err := os.ReadFile(countPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("failed to read the invocation count: %v", err)
	}
	return len(data)
}
