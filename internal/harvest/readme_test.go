package harvest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

const sampleREADME = `# Utopia

## Quick Start
Run utopia init to set up, then utopia cr to converse and utopia execute to apply.

The Loop: Converse -> Execute -> Harvest

Knowledge Artifacts include ADRs, Concepts, and Domain docs.

## Project Structure
` + "```" + `
.utopia/
├── specs/
├── adrs/
├── concepts/
` + "```" + `
`

func TestParseREADMEDocumented(t *testing.T) {
	doc := parseREADMEDocumented(sampleREADME)

	for _, cmd := range []string{"init", "cr", "execute"} {
		found := false
		for _, c := range doc.Commands {
			if c == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %q in %v", cmd, doc.Commands)
		}
	}

	if len(doc.ArtifactTypes) != 3 {
		t.Errorf("expected 3 artifact types, got %v", doc.ArtifactTypes)
	}

	for _, dir := range []string{"specs", "adrs", "concepts"} {
		found := false
		for _, d := range doc.Directories {
			if d == dir {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected directory %q in %v", dir, doc.Directories)
		}
	}

	if len(doc.WorkflowSteps) != 3 {
		t.Errorf("expected 3 workflow steps, got %v", doc.WorkflowSteps)
	}
}

func TestParseREADMEDocumented_DeduplicatesCommands(t *testing.T) {
	doc := parseREADMEDocumented("utopia cr and utopia cr again")
	if len(doc.Commands) != 1 || doc.Commands[0] != "cr" {
		t.Errorf("expected deduplicated [cr], got %v", doc.Commands)
	}
}

func TestDetectREADMESignals_NewCommandQualifies(t *testing.T) {
	documented := &readmeDocumented{Commands: []string{"cr", "execute"}}
	specs := []*domain.Spec{{
		ID: "adoption",
		Features: []domain.Feature{{
			ID:          "prune-command",
			Description: "New command: utopia prune command removes stale draft specs",
		}},
	}}

	candidates := detectREADMESignals(specs, documented)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	c := candidates[0]
	if c.Category != "command" {
		t.Errorf("Category = %q, want \"command\"", c.Category)
	}
	if c.Title != "utopia prune command" {
		t.Errorf("Title = %q, want \"utopia prune command\"", c.Title)
	}
	if c.Confidence != domain.SignalConfidenceHigh {
		t.Errorf("Confidence = %q, want high", c.Confidence)
	}
	if c.SpecID != "adoption" || c.FeatureID != "prune-command" {
		t.Errorf("source = %s/%s, want adoption/prune-command", c.SpecID, c.FeatureID)
	}
}

func TestDetectREADMESignals_DocumentedCommandExcluded(t *testing.T) {
	documented := &readmeDocumented{Commands: []string{"prune"}}
	specs := []*domain.Spec{{
		ID: "adoption",
		Features: []domain.Feature{{
			ID:          "prune-command",
			Description: "New command: utopia prune command removes stale draft specs",
		}},
	}}

	if candidates := detectREADMESignals(specs, documented); len(candidates) != 0 {
		t.Errorf("expected no candidates for already-documented command, got %v", candidates)
	}
}

func TestQualifyFeatureForREADME_Disqualifications(t *testing.T) {
	documented := &readmeDocumented{Commands: []string{"cr"}}
	spec := &domain.Spec{ID: "adoption"}

	tests := []struct {
		name    string
		feature domain.Feature
	}{
		{
			name:    "enhancement language",
			feature: domain.Feature{ID: "better-output", Description: "Extend the harvest workflow to add a new phase"},
		},
		{
			name:    "enhancement to documented command",
			feature: domain.Feature{ID: "cr-improvements", Description: "The utopia cr command gets richer summaries"},
		},
		{
			name:    "internal implementation",
			feature: domain.Feature{ID: "yaml-marshaler", Description: "Internal yaml marshaler for the storage layer"},
		},
		{
			name:    "config option",
			feature: domain.Feature{ID: "model-flag", Description: "A --model flag selects the Claude model"},
		},
		{
			name:    "spec-only change",
			feature: domain.Feature{ID: "criteria", Description: "Changes the acceptance criteria format in specs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualifyFeatureForREADME(spec, tt.feature, documented); got != nil {
				t.Errorf("expected disqualification, got candidate %+v", got)
			}
		})
	}
}

func TestQualifyFeatureForREADME_NewDirectory(t *testing.T) {
	documented := &readmeDocumented{Directories: []string{"specs", "adrs"}}
	spec := &domain.Spec{ID: "notes"}
	feature := domain.Feature{
		ID:          "session-notes",
		Description: "Creates a new .utopia/notes directory for session notes",
	}

	got := qualifyFeatureForREADME(spec, feature, documented)
	if got == nil {
		t.Fatal("expected a directory candidate, got nil")
	}
	if got.Category != "directory" {
		t.Errorf("Category = %q, want \"directory\"", got.Category)
	}
	if got.Title != ".utopia/notes/" {
		t.Errorf("Title = %q, want \".utopia/notes/\"", got.Title)
	}
	if got.SuggestedSection != "Project Structure" {
		t.Errorf("SuggestedSection = %q, want \"Project Structure\"", got.SuggestedSection)
	}
}

func TestQualifyFeatureForREADME_DocumentedDirectoryExcluded(t *testing.T) {
	documented := &readmeDocumented{Directories: []string{"notes"}}
	spec := &domain.Spec{ID: "notes"}
	feature := domain.Feature{
		ID:          "session-notes",
		Description: "Creates a new .utopia/notes directory for session notes",
	}

	if got := qualifyFeatureForREADME(spec, feature, documented); got != nil {
		t.Errorf("expected nil for already-documented directory, got %+v", got)
	}
}

func TestExtractCommandName(t *testing.T) {
	tests := []struct {
		name    string
		feature domain.Feature
		want    string
	}{
		{
			name:    "from description",
			feature: domain.Feature{ID: "f1", Description: "Adds utopia prune to the CLI"},
			want:    "utopia prune command",
		},
		{
			name:    "from feature ID suffix",
			feature: domain.Feature{ID: "prune-command", Description: "removes stale drafts"},
			want:    "utopia prune command",
		},
		{
			name:    "fallback to ID",
			feature: domain.Feature{ID: "something-else", Description: "no pattern here"},
			want:    "something-else",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractCommandName(tt.feature); got != tt.want {
				t.Errorf("extractCommandName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatREADMESignalsSummary_Empty(t *testing.T) {
	got := formatREADMESignalsSummary(nil)
	if got != "(No README documentation signals found - all qualifying features are already documented)" {
		t.Errorf("unexpected empty summary: %q", got)
	}
}

func TestFormatREADMESignalsSummary_GroupsByCategory(t *testing.T) {
	candidates := []readmeSignalCandidate{
		{SpecID: "s1", FeatureID: "f1", Title: ".utopia/notes/", Category: "directory", Confidence: domain.SignalConfidenceHigh, SuggestedSection: "Project Structure"},
		{SpecID: "s2", FeatureID: "f2", Title: "utopia prune command", Category: "command", Confidence: domain.SignalConfidenceHigh, SuggestedSection: "Quick Start / The Loop"},
	}

	got := formatREADMESignalsSummary(candidates)

	if !strings.Contains(got, "**2 potential README documentation signals found:**") {
		t.Errorf("missing count line, got:\n%s", got)
	}
	cmdIdx := strings.Index(got, "### New CLI Commands")
	dirIdx := strings.Index(got, "### New .utopia/ Directories")
	if cmdIdx == -1 || dirIdx == -1 {
		t.Fatalf("missing category headers, got:\n%s", got)
	}
	if cmdIdx > dirIdx {
		t.Error("commands category should be listed before directories")
	}
	if !strings.Contains(got, "- **utopia prune command** (spec: s2, feature: f2)") {
		t.Errorf("missing candidate line, got:\n%s", got)
	}
}

func TestBuildREADMESignalsSummary_MissingREADME(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewYAMLStore(filepath.Join(dir, ".utopia"))

	got := buildREADMESignalsSummary(dir, store)
	if got != "(Could not read README.md - README signals skipped)" {
		t.Errorf("unexpected summary: %q", got)
	}
	if count := countREADMESignals(dir, store); count != 0 {
		t.Errorf("countREADMESignals = %d, want 0", count)
	}
}

func TestCountREADMESignals_WithREADME(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(sampleREADME), 0644); err != nil {
		t.Fatal(err)
	}
	store := internal.NewYAMLStore(filepath.Join(dir, ".utopia"))

	// No specs exist, so no signals regardless of README content.
	if count := countREADMESignals(dir, store); count != 0 {
		t.Errorf("countREADMESignals = %d, want 0", count)
	}
}
