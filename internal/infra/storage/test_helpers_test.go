package storage

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/testutil"
)

// SetupTestStore creates a YAMLStore backed by a temporary directory with the
// necessary .utopia structure (specs/ and change-requests/ subdirectories).
// Returns the store and a cleanup function.
func SetupTestStore(t *testing.T) (*YAMLStore, func()) {
	t.Helper()
	dir, cleanup := testutil.SetupTestProject(t)
	return NewYAMLStore(dir), cleanup
}
