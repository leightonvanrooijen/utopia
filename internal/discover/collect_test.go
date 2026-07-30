package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectCodebaseContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "sub/helper.go", "package sub")
	writeFile(t, dir, ".git/config", "[core]")
	writeFile(t, dir, "vendor/dep.go", "package dep")
	if err := os.WriteFile(filepath.Join(dir, "image.bin"), []byte{0x00, 0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}

	context, files, err := collectCodebaseContext(dir, Scope{}, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	for _, f := range files {
		if strings.Contains(f, ".git") || strings.Contains(f, "vendor") || strings.Contains(f, "image.bin") {
			t.Errorf("unexpected file collected: %s", f)
		}
	}
	if !strings.Contains(context, "**File: main.go**") {
		t.Errorf("context missing main.go section")
	}
	if !strings.Contains(context, "### Source Files") {
		t.Errorf("context missing Source Files header")
	}
}

func TestCollectCodebaseContext_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "main_test.go", "package main")

	scope := Scope{ExcludePatterns: []string{"*_test.go"}}
	_, files, err := collectCodebaseContext(dir, scope, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("expected only main.go, got %v", files)
	}
}

func TestCollectCodebaseContext_ScopedPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "root.go", "package main")
	writeFile(t, dir, "sub/inner.go", "package sub")

	scope := Scope{Paths: []string{"sub"}}
	_, files, err := collectCodebaseContext(dir, scope, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join("sub", "inner.go") {
		t.Errorf("expected only sub/inner.go, got %v", files)
	}
}

func TestCollectCodebaseContext_EmptyDir(t *testing.T) {
	_, files, err := collectCodebaseContext(t.TempDir(), Scope{}, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files, got %v", files)
	}
}

func TestCollectDomainContextIncremental_SkipsNonTypeFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "types.go", "package main\n\ntype Widget struct{}\n")
	writeFile(t, dir, "types_test.go", "package main")
	writeFile(t, dir, "widget_mock.go", "package main")
	writeFile(t, dir, "api.gen.go", "package main")
	writeFile(t, dir, "readme.txt", "not a type file")

	context, files, err := collectDomainContextIncremental(dir, time.Time{}, false, Scope{}, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if _, ok := files["types.go"]; !ok {
		t.Errorf("expected types.go to be analyzed, got %v", files)
	}
	if !strings.Contains(context, "### Go Type Definitions") {
		t.Errorf("context missing Go Type Definitions section")
	}
}

func TestCollectDomainContextIncremental_IncrementalSkipsOldFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "old.go", "package main")
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "old.go"), past, past); err != nil {
		t.Fatal(err)
	}

	lastRun := time.Now().Add(-1 * time.Hour)
	_, files, err := collectDomainContextIncremental(dir, lastRun, true, Scope{}, newProgress(nil, 4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files in incremental mode, got %v", files)
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"internal/store.go", "**/*.go", true},
		{"store.go", "**/*.go", true},
		{"internal/store.ts", "**/*.go", false},
		{"vendor/dep/file.go", "**/vendor/**", true},
		{"vendor", "**/vendor/**", true},
		{"internal/file.go", "**/vendor/**", false},
		{"a/b/c.go", "*.go", false},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.path, tt.pattern); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	patterns := []string{"*_test.go", "**/generated/**"}
	if !matchesAnyPattern("internal/store_test.go", patterns) {
		t.Error("expected *_test.go to match by base name")
	}
	if !matchesAnyPattern("generated/api.go", patterns) {
		t.Error("expected generated dir to match")
	}
	if matchesAnyPattern("internal/store.go", patterns) {
		t.Error("expected store.go not to match")
	}
	if matchesAnyPattern("internal/store.go", nil) {
		t.Error("expected no match with empty patterns")
	}
}

func TestIsTextFile(t *testing.T) {
	if !isTextFile([]byte("plain text")) {
		t.Error("expected plain text to be a text file")
	}
	if !isTextFile([]byte{}) {
		t.Error("expected empty content to be a text file")
	}
	if isTextFile([]byte{0x89, 0x50, 0x00, 0x47}) {
		t.Error("expected content with null byte to be binary")
	}
}

func TestTruncateContent(t *testing.T) {
	if got := truncateContent("short", 10); got != "short" {
		t.Errorf("expected untouched content, got %q", got)
	}
	got := truncateContent("0123456789", 5)
	if got != "01234\n... [truncated]" {
		t.Errorf("unexpected truncation: %q", got)
	}
}

func TestBuildExistingSpecsSummary(t *testing.T) {
	if got := buildExistingSpecsSummary(nil); got != "(No existing specifications)" {
		t.Errorf("unexpected empty summary: %q", got)
	}
	specs := []*domain.Spec{{
		ID:          "spec-1",
		Title:       "Widget Creation",
		Description: "Users can create widgets",
		Features:    []domain.Feature{{ID: "f1", Description: "Create via CLI"}},
	}}
	summary := buildExistingSpecsSummary(specs)
	for _, want := range []string{"Widget Creation", "spec-1", "f1: Create via CLI"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func TestBuildExistingDomainDocsSummary(t *testing.T) {
	if got := buildExistingDomainDocsSummary(nil); got != "(No existing domain documents)" {
		t.Errorf("unexpected empty summary: %q", got)
	}
	docs := []*domain.DomainDoc{{
		Title:          "Widget Context",
		BoundedContext: "widget",
		Description:    "Owns widgets",
		Terms:          []domain.DomainTerm{{Term: "Widget", Definition: "A thing"}},
	}}
	summary := buildExistingDomainDocsSummary(docs)
	for _, want := range []string{"Widget Context", "widget", "Widget: A thing"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func TestTitleCase(t *testing.T) {
	if got := titleCase("high"); got != "High" {
		t.Errorf("expected High, got %q", got)
	}
	if got := titleCase(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
