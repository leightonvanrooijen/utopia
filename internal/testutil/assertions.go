package testutil

import (
	"strings"
	"testing"
)

// AssertNoError fails the test if err is not nil.
// Uses t.Helper() so failure reports show the caller's line number.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertErrorContains fails the test if err is nil or if err.Error() does not
// contain the expected substring.
// Uses t.Helper() so failure reports show the caller's line number.
func AssertErrorContains(t *testing.T, err error, substring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		t.Fatalf("expected error containing %q, got: %v", substring, err)
	}
}

// AssertContains fails the test if haystack does not contain needle.
// Uses t.Helper() so failure reports show the caller's line number.
func AssertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}
