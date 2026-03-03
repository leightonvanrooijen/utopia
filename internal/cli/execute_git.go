package cli

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// gitCommitChunk creates a git commit for newly generated work items.
// Only stages and commits the work items for this specific CR, not pre-existing items.
func gitCommitChunk(projectDir, crID string) error {
	// Stage only the work items for this CR
	workItemsDir := filepath.Join(projectDir, ".utopia", "work-items", crID)
	addCmd := exec.Command("git", "add", workItemsDir)
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w (%s)", err, addStderr.String())
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit with chunk message format
	msg := fmt.Sprintf("chunk: %s", crID)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}

// gitCommitCleanup creates a git commit for the removal of CR and work items after merge.
// Stages removal of .utopia/work-items/<cr-id>/ and .utopia/change-requests/<cr-id>.yaml
func gitCommitCleanup(projectDir, crID, utopiaDir string) error {
	// Stage removal of work items directory
	workItemsDir := filepath.Join(utopiaDir, "work-items", crID)
	addWorkItemsCmd := exec.Command("git", "add", workItemsDir)
	addWorkItemsCmd.Dir = projectDir
	// Ignore errors - directory may not exist or may already be staged

	var addStderr bytes.Buffer
	addWorkItemsCmd.Stderr = &addStderr
	addWorkItemsCmd.Run() // Best effort

	// Stage removal of CR file
	crFile := filepath.Join(utopiaDir, "change-requests", crID+".yaml")
	addCRCmd := exec.Command("git", "add", crFile)
	addCRCmd.Dir = projectDir
	addCRCmd.Stderr = &addStderr
	addCRCmd.Run() // Best effort

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit with cleanup message format
	msg := fmt.Sprintf("cleanup: complete %s", crID)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}

// GitCommitCR creates a git commit for a newly validated CR.
// Returns the commit SHA on success, or error describing the failure.
func GitCommitCR(projectDir, crID string) (string, error) {
	// Stage the CR file
	crFile := filepath.Join(projectDir, ".utopia", "change-requests", crID+".yaml")
	addCmd := exec.Command("git", "add", crFile)
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return "", fmt.Errorf("git add failed: %w (%s)", err, addStderr.String())
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return "", nil
	}

	// Commit with standard message pattern
	msg := fmt.Sprintf("cr: create %s", crID)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return "", fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	// Get the commit SHA
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = projectDir
	var shaOut bytes.Buffer
	shaCmd.Stdout = &shaOut
	if err := shaCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get commit SHA: %w", err)
	}

	return strings.TrimSpace(shaOut.String()), nil
}

// getGitBranch returns the current git branch name
func getGitBranch(projectDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out.String())
}

// GitCommitSpecMerge creates a git commit for spec merge changes.
// Returns nil if commit succeeds, or error describing the failure.
func GitCommitSpecMerge(projectDir string, cr *domain.ChangeRequest, mergeResult *MergeResult) error {
	// Build commit message
	var msg string
	if mergeResult.IsRefactor {
		msg = fmt.Sprintf("spec: merge refactor CR '%s'\n\nNo spec modifications (refactor only).", cr.Title)
	} else {
		msg = fmt.Sprintf("spec: merge CR '%s'", cr.Title)
		if len(mergeResult.SpecsModified) > 0 || len(mergeResult.SpecsDeleted) > 0 {
			msg += "\n\nModified specs:"
			for _, s := range mergeResult.SpecsModified {
				msg += fmt.Sprintf("\n  - %s", s)
			}
			for _, s := range mergeResult.SpecsDeleted {
				msg += fmt.Sprintf("\n  - %s (deleted)", s)
			}
		}
	}

	// Stage spec changes
	specsDir := filepath.Join(projectDir, ".utopia", "specs")
	addCmd := exec.Command("git", "add", specsDir)
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w (%s)", err, addStderr.String())
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}
