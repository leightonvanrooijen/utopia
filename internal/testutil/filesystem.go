package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// SetupTestProject creates a temporary directory with the necessary subdirectories
// for testing YAML storage operations (.utopia structure with specs/ and change-requests/).
// Returns the directory path and a cleanup function.
func SetupTestProject(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "utopia-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create necessary subdirectories
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("failed to create specs subdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "change-requests"), 0755); err != nil {
		t.Fatalf("failed to create change-requests subdir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup
}

// SetupTestDir creates a temporary directory with the necessary subdirectories
// for testing YAML storage operations. Returns the directory path and a cleanup function.
//
// Deprecated: Use SetupTestProject instead.
func SetupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	return SetupTestProject(t)
}
