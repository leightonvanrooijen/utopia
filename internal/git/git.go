// Package git provides reusable git operations for the utopia project.
package git

import (
	"bytes"
	"os/exec"
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
// Returns nil if no changes were staged (no commit created).
func CommitIfChanged(projectDir, message string, paths ...string) error {
	if err := Add(projectDir, paths...); err != nil {
		return err
	}
	if !HasStagedChanges(projectDir) {
		return nil
	}
	return Commit(projectDir, message)
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
