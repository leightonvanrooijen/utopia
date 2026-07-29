package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// mergeCleanupSequence reproduces AutoMergeCR's post-merge steps for a CR that
// is already tracked in git: resolve the real on-disk path, run the real
// CleanupAfterMerge (which sets status to complete and *saves* before deleting),
// then commit the cleanup. The status save is the step that used to fork an
// un-prefixed shadow file and steal resolution from the delete, so a test that
// skips it cannot see the bug. It returns the resolved path.
func mergeCleanupSequence(t *testing.T, projectDir string, store *internal.YAMLStore, crID string) string {
	t.Helper()
	utopiaDir := filepath.Join(projectDir, ".utopia")

	cr, err := store.ResolveChangeRequest(crID)
	if err != nil {
		t.Fatalf("failed to resolve CR: %v", err)
	}
	crFile, err := store.ChangeRequestPath(crID)
	if err != nil {
		t.Fatalf("failed to resolve CR path: %v", err)
	}
	if err := CleanupAfterMerge(cr, crID, utopiaDir, store); err != nil {
		t.Fatalf("CleanupAfterMerge failed: %v", err)
	}
	if err := gitCommitCleanup(projectDir, crFile, crID, utopiaDir); err != nil {
		t.Fatalf("failed to commit cleanup: %v", err)
	}
	return crFile
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

	crFile := mergeCleanupSequence(t, projectDir, store, "reusable-core")
	if want := filepath.Join(utopiaDir, "change-requests", "01_reusable-core.yaml"); crFile != want {
		t.Fatalf("resolved CR path = %q, want %q", crFile, want)
	}

	if got, want := lastCommitSubject(t, projectDir), "cleanup: complete reusable-core"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}

	// The commit records the prefixed file's deletion rather than an empty
	// change set (CommitIfChanged skips entirely when nothing is staged, so a
	// stale subject here would mean the previous commit, not a cleanup commit).
	status := exec.Command("git", "show", "--name-status", "--format=", "HEAD")
	status.Dir = projectDir
	statusOut, err := status.Output()
	if err != nil {
		t.Fatalf("git show failed: %v", err)
	}
	if want := "D\t.utopia/change-requests/01_reusable-core.yaml"; !strings.Contains(string(statusOut), want) {
		t.Errorf("cleanup commit change set = %q, want it to record %q", string(statusOut), want)
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

// Regression: cleanup used to leave an orphan behind for a prefix-named CR.
// Setting status to complete wrote change-requests/reusable-core.yaml, which
// outranked 01_reusable-core.yaml in resolution, so the delete removed the
// shadow it had just created and the real file survived with a stale
// in-progress status. Because ListChangeRequests applies no status filter, that
// orphan was then picked up by "utopia execute --all" and - its work items gone
// - re-chunked, re-executed and re-merged, sorting first thanks to its prefix.
func TestCleanupAfterMerge_PrefixedCRLeavesNoOrphan(t *testing.T) {
	projectDir, store := setupCRRepo(t, "01_reusable-core.yaml", "reusable-core")
	if _, err := GitCommitCR(projectDir, store, "reusable-core"); err != nil {
		t.Fatalf("failed to commit CR: %v", err)
	}

	mergeCleanupSequence(t, projectDir, store, "reusable-core")

	// Nothing with this id remains anywhere in change-requests/: neither the
	// un-prefixed shadow nor the prefixed file itself.
	crDir := filepath.Join(projectDir, ".utopia", "change-requests")
	entries, err := os.ReadDir(crDir)
	if err != nil {
		t.Fatalf("failed to read change-requests dir: %v", err)
	}
	for _, entry := range entries {
		t.Errorf("change-requests should be empty after cleanup, found %q", entry.Name())
	}

	// The work list "utopia execute --all" builds is empty, so the merged CR
	// cannot be re-chunked or re-executed.
	crs, err := store.ListChangeRequests()
	if err != nil {
		t.Fatalf("failed to list change requests: %v", err)
	}
	if len(crs) != 0 {
		t.Errorf("ListChangeRequests returned %d CR(s) after cleanup, want 0 (an orphan would be re-executed)", len(crs))
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
