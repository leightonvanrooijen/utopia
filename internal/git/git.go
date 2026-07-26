// Package git provides reusable git operations for the utopia project.
package git

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Add stages files for commit.
// paths are relative to projectDir or absolute paths.
func Add(projectDir string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &GitError{Op: "add", Err: err, Stderr: stderr.String()}
	}
	return nil
}

// HasStagedChanges returns true if there are staged changes ready to commit.
func HasStagedChanges(projectDir string) bool {
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = projectDir
	// Exit code 0 = no changes, non-zero = has changes
	return cmd.Run() != nil
}

// Commit creates a commit with the given message.
// Returns an error if the commit fails.
func Commit(projectDir, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = projectDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &GitError{Op: "commit", Err: err, Stderr: stderr.String()}
	}
	return nil
}

// CommitIfChanged stages the given paths and commits if there are changes.
// This is the most common pattern: stage specific paths, then commit only if
// something was actually staged.
// Paths that cannot be staged (nonexistent and untracked, or ignored and
// untracked) are skipped so they don't abort staging of the remaining paths.
// Tracked paths deleted from disk are staged as deletions.
// Returns nil if no changes were staged (no commit created).
func CommitIfChanged(projectDir, message string, paths ...string) error {
	stageable := stageablePaths(projectDir, paths)
	if len(stageable) > 0 {
		if err := Add(projectDir, stageable...); err != nil {
			return err
		}
	}
	if !HasStagedChanges(projectDir) {
		return nil
	}
	return Commit(projectDir, message)
}

// stageablePaths filters paths down to those git add can stage without a
// fatal error: paths tracked in the index (including deleted tracked files,
// whose removal is staged) and paths that exist on disk and are not ignored.
func stageablePaths(projectDir string, paths []string) []string {
	var stageable []string
	for _, path := range paths {
		if isTracked(projectDir, path) {
			stageable = append(stageable, path)
			continue
		}
		onDisk := path
		if !filepath.IsAbs(onDisk) {
			onDisk = filepath.Join(projectDir, onDisk)
		}
		if _, err := os.Stat(onDisk); err != nil {
			continue
		}
		if isIgnored(projectDir, path) {
			continue
		}
		stageable = append(stageable, path)
	}
	return stageable
}

// isTracked returns true if the path (file or directory) contains at least
// one file tracked in the git index.
func isTracked(projectDir, path string) bool {
	cmd := exec.Command("git", "ls-files", "--", path)
	cmd.Dir = projectDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) != ""
}

// isIgnored returns true if the path is excluded by gitignore rules.
func isIgnored(projectDir, path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", "--", path)
	cmd.Dir = projectDir
	return cmd.Run() == nil
}

// CurrentBranch returns the name of the current git branch.
// Returns "unknown" if the branch cannot be determined.
func CurrentBranch(projectDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(stdout.String())
}

// HeadSHA returns the SHA of the current HEAD commit.
func HeadSHA(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = projectDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &GitError{Op: "rev-parse HEAD", Err: err, Stderr: stderr.String()}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GitError represents a git command error with stderr output.
type GitError struct {
	Op     string
	Err    error
	Stderr string
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return "git " + e.Op + " failed: " + e.Err.Error() + " (" + strings.TrimSpace(e.Stderr) + ")"
	}
	return "git " + e.Op + " failed: " + e.Err.Error()
}

func (e *GitError) Unwrap() error {
	return e.Err
}
