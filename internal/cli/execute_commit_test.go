package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
)

// setupCRRepo creates a temporary git repo with an initialized .utopia project
// and writes a raw change request under the given filename with the given
// internal id. It returns the project dir and a store rooted at .utopia.
func setupCRRepo(t *testing.T, filename, id string) (string, *internal.YAMLStore) {
	t.Helper()
	projectDir := t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = projectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	crDir := filepath.Join(projectDir, ".utopia", "change-requests")
	if err := os.MkdirAll(crDir, 0755); err != nil {
		t.Fatalf("failed to create change-requests dir: %v", err)
	}
	content := "id: " + id + "\ntype: refactor\ntitle: " + id + "\nstatus: approved\n"
	if err := os.WriteFile(filepath.Join(crDir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write CR file: %v", err)
	}

	return projectDir, internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
}

func lastCommitSubject(t *testing.T, projectDir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	return string(out[:len(out)-1]) // drop trailing newline
}

// A CR saved under a numeric-prefixed filename must still commit, keyed to its
// internal id in both the staged file (AC1) and the message (AC3).
func TestGitCommitCR_NumericPrefix(t *testing.T) {
	projectDir, store := setupCRRepo(t, "01_reusable-core.yaml", "reusable-core")

	sha, err := GitCommitCR(projectDir, store, "reusable-core")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a commit SHA, got empty")
	}
	if got, want := lastCommitSubject(t, projectDir), "cr: create reusable-core"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}

	// The real on-disk file (with its prefix) is what got committed.
	tracked := exec.Command("git", "ls-files", ".utopia/change-requests")
	tracked.Dir = projectDir
	out, err := tracked.Output()
	if err != nil {
		t.Fatalf("git ls-files failed: %v", err)
	}
	if got := string(out); got != ".utopia/change-requests/01_reusable-core.yaml\n" {
		t.Errorf("tracked CR files = %q, want the prefixed file", got)
	}
}

// An ambiguous id (two prefixed files, no canonical) must fail before any
// commit rather than silently staging one of them (AC2).
func TestGitCommitCR_AmbiguousFails(t *testing.T) {
	projectDir, store := setupCRRepo(t, "06_ai-chat.yaml", "ai-chat")
	crDir := filepath.Join(projectDir, ".utopia", "change-requests")
	if err := os.WriteFile(filepath.Join(crDir, "07_ai-chat.yaml"), []byte("id: ai-chat\ntype: refactor\ntitle: ai-chat\nstatus: approved\n"), 0644); err != nil {
		t.Fatalf("failed to write second CR file: %v", err)
	}

	if _, err := GitCommitCR(projectDir, store, "ai-chat"); err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}

	// No commit should have been created (repo has no HEAD yet).
	head := exec.Command("git", "rev-parse", "--verify", "HEAD")
	head.Dir = projectDir
	if err := head.Run(); err == nil {
		t.Error("expected no commit to exist after ambiguous resolution")
	}
}
