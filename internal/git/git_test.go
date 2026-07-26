package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo creates a temporary git repository with an initial commit
// containing a tracked work-items directory.
func setupRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")

	if err := os.MkdirAll(filepath.Join(tmpDir, "work-items", "cr-1"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "work-items", "cr-1", "item.yaml"), []byte("id: item-1\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "cr-1.yaml"), []byte("id: cr-1\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	runGit("add", ".")
	runGit("commit", "-m", "initial commit")
	return tmpDir
}

func lastCommitMessage(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func workingTreeClean(t *testing.T, dir string) bool {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	return strings.TrimSpace(string(out)) == ""
}

func TestCommitIfChangedStagesDeletionsOfRemovedPaths(t *testing.T) {
	tmpDir := setupRepo(t)

	// Simulate merge cleanup: CR file and work items already removed from disk.
	if err := os.RemoveAll(filepath.Join(tmpDir, "work-items", "cr-1")); err != nil {
		t.Fatalf("failed to remove dir: %v", err)
	}
	if err := os.Remove(filepath.Join(tmpDir, "cr-1.yaml")); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	workItemsDir := filepath.Join(tmpDir, "work-items", "cr-1")
	crFile := filepath.Join(tmpDir, "cr-1.yaml")
	if err := CommitIfChanged(tmpDir, "cleanup: complete cr-1", workItemsDir, crFile); err != nil {
		t.Fatalf("CommitIfChanged failed: %v", err)
	}

	if got := lastCommitMessage(t, tmpDir); got != "cleanup: complete cr-1" {
		t.Errorf("expected cleanup commit, got %q", got)
	}
	if !workingTreeClean(t, tmpDir) {
		t.Errorf("expected clean working tree after cleanup commit")
	}
}

func TestCommitIfChangedSkipsNonexistentAndIgnoredPaths(t *testing.T) {
	tmpDir := setupRepo(t)

	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("ignored/\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "ignored"), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "ignored", "f.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "new.yaml"), []byte("id: new\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	err := CommitIfChanged(tmpDir, "chunk: cr-2",
		filepath.Join(tmpDir, "does-not-exist"),
		filepath.Join(tmpDir, "ignored"),
		filepath.Join(tmpDir, "new.yaml"),
	)
	if err != nil {
		t.Fatalf("CommitIfChanged failed: %v", err)
	}

	if got := lastCommitMessage(t, tmpDir); got != "chunk: cr-2" {
		t.Errorf("expected chunk commit, got %q", got)
	}
}

func TestCommitIfChangedNoCommitWhenNothingStaged(t *testing.T) {
	tmpDir := setupRepo(t)

	before := lastCommitMessage(t, tmpDir)
	err := CommitIfChanged(tmpDir, "cleanup: complete cr-x",
		filepath.Join(tmpDir, "work-items", "cr-x"),
		filepath.Join(tmpDir, "cr-x.yaml"),
	)
	if err != nil {
		t.Fatalf("CommitIfChanged failed: %v", err)
	}
	if got := lastCommitMessage(t, tmpDir); got != before {
		t.Errorf("expected no new commit, got %q", got)
	}
}
