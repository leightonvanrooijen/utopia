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

// The post-merge cleanup commit must stage the removal of the CR's real
// on-disk file, which may carry a numeric ordering prefix. The path is
// resolved before deletion (as AutoMergeCR does) and threaded to the commit,
// so a CR saved as 01_reusable-core.yaml has its deletion recorded. A path
// reconstructed as change-requests/reusable-core.yaml would never match the
// tracked file, leaving it tracked and the removal uncommitted.
func TestGitCommitCleanup_NumericPrefix(t *testing.T) {
	projectDir, store := setupCRRepo(t, "01_reusable-core.yaml", "reusable-core")
	utopiaDir := filepath.Join(projectDir, ".utopia")

	// Track the prefixed CR file so its removal is something git can stage.
	if _, err := GitCommitCR(projectDir, store, "reusable-core"); err != nil {
		t.Fatalf("failed to commit CR: %v", err)
	}

	// Mirror AutoMergeCR: resolve the real path, then delete, then commit the
	// cleanup. Resolution must find the prefixed file, not reusable-core.yaml.
	crFile, err := store.ChangeRequestPath("reusable-core")
	if err != nil {
		t.Fatalf("failed to resolve CR path: %v", err)
	}
	if want := filepath.Join(utopiaDir, "change-requests", "01_reusable-core.yaml"); crFile != want {
		t.Fatalf("resolved CR path = %q, want %q", crFile, want)
	}
	if err := store.DeleteChangeRequest("reusable-core"); err != nil {
		t.Fatalf("failed to delete CR: %v", err)
	}

	if err := gitCommitCleanup(projectDir, crFile, "reusable-core", utopiaDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := lastCommitSubject(t, projectDir), "cleanup: complete reusable-core"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}

	// The prefixed CR file's removal was committed: it is no longer tracked.
	tracked := exec.Command("git", "ls-files", ".utopia/change-requests")
	tracked.Dir = projectDir
	out, err := tracked.Output()
	if err != nil {
		t.Fatalf("git ls-files failed: %v", err)
	}
	if got := string(out); got != "" {
		t.Errorf("tracked CR files = %q, want none (removal committed)", got)
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
