package testutil

import (
	"errors"
	"testing"
)

func TestAssertNoError_NoError(t *testing.T) {
	// Should not fail when error is nil
	AssertNoError(t, nil)
}

func TestAssertErrorContains_MatchingSubstring(t *testing.T) {
	err := errors.New("connection failed: timeout")
	// Should not fail when substring is present
	AssertErrorContains(t, err, "timeout")
}

func TestAssertErrorContains_FullMessage(t *testing.T) {
	err := errors.New("exact message")
	// Should work with full message match
	AssertErrorContains(t, err, "exact message")
}

func TestAssertContains_Present(t *testing.T) {
	// Should not fail when needle is in haystack
	AssertContains(t, "hello world", "world")
}

func TestAssertContains_Exact(t *testing.T) {
	// Should work with exact match
	AssertContains(t, "hello", "hello")
}

func TestAssertContains_Empty(t *testing.T) {
	// Empty string is contained in any string
	AssertContains(t, "hello", "")
}
