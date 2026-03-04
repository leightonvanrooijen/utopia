package comparison

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/strategies/readme"
)

func TestNew(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New returned nil")
	}
}

func TestStrategy_Name(t *testing.T) {
	s := New()
	if s.Name() != "comparison" {
		t.Errorf("Name() = %q, want %q", s.Name(), "comparison")
	}
}

func TestStrategy_Description(t *testing.T) {
	s := New()
	if s.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestStrategy_Detect_EmptySpecs(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{}

	candidates := s.Detect([]*domain.Spec{}, documented)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for empty specs, got %d", len(candidates))
	}
}

func TestStrategy_Detect_NewCommand(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		Commands: []string{"cr", "execute"}, // "status" not documented
	}

	specs := []*domain.Spec{
		{
			ID: "adoption",
			Features: []domain.Feature{
				{
					ID:          "status-command",
					Description: "CLI command to run utopia status for viewing project state",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	// The command must match "utopia <cmd>" pattern in description, or have "-command" suffix in ID
	// Our test case has status-command ID, so it should qualify as a new command
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].Category != "command" {
		t.Errorf("Category = %q, want %q", candidates[0].Category, "command")
	}

	if candidates[0].Confidence != domain.SignalConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", candidates[0].Confidence, domain.SignalConfidenceHigh)
	}
}

func TestStrategy_Detect_DocumentedCommand_NoSignal(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		Commands: []string{"cr", "execute", "status"}, // status already documented
	}

	specs := []*domain.Spec{
		{
			ID: "adoption",
			Features: []domain.Feature{
				{
					ID:          "status-command",
					Description: "CLI command to run utopia status",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for already documented command, got %d", len(candidates))
	}
}

func TestStrategy_Detect_InternalImplementation_Disqualified(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{}

	specs := []*domain.Spec{
		{
			ID: "internal-stuff",
			Features: []domain.Feature{
				{
					ID:          "yaml-parser",
					Description: "Internal helper for parsing YAML files",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for internal implementation, got %d", len(candidates))
	}
}

func TestStrategy_Detect_Enhancement_Disqualified(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		Commands: []string{"cr"},
	}

	specs := []*domain.Spec{
		{
			ID: "cr-enhancements",
			Features: []domain.Feature{
				{
					ID:          "cr-improvements",
					Description: "Extend the utopia cr command with better error handling",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for enhancement to existing command, got %d", len(candidates))
	}
}

func TestStrategy_Detect_ConfigOption_Disqualified(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{}

	specs := []*domain.Spec{
		{
			ID: "config",
			Features: []domain.Feature{
				{
					ID:          "verbose-flag",
					Description: "Configuration option for verbose output",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for config option, got %d", len(candidates))
	}
}

func TestStrategy_Detect_NewArtifactType(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		ArtifactTypes: []string{"ADR", "Concept", "Domain"},
	}

	specs := []*domain.Spec{
		{
			ID: "new-artifact",
			Features: []domain.Feature{
				{
					ID:          "metrics-artifact",
					Description: "New knowledge artifact type harvested from conversations about performance metrics",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].Category != "artifact" {
		t.Errorf("Category = %q, want %q", candidates[0].Category, "artifact")
	}
}

func TestStrategy_Detect_WorkflowChange(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		WorkflowSteps: []string{"converse", "execute", "harvest"},
	}

	specs := []*domain.Spec{
		{
			ID: "workflow-update",
			Features: []domain.Feature{
				{
					ID:          "review-phase",
					Description: "Add new phase to the core loop for reviewing changes before commit",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].Category != "workflow" {
		t.Errorf("Category = %q, want %q", candidates[0].Category, "workflow")
	}

	if candidates[0].Confidence != domain.SignalConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", candidates[0].Confidence, domain.SignalConfidenceMedium)
	}
}

func TestStrategy_Detect_NewDirectory(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		Directories: []string{"specs", "workitems", "adrs"},
	}

	specs := []*domain.Spec{
		{
			ID: "new-dir",
			Features: []domain.Feature{
				{
					ID:          "reports-dir",
					Description: "Store reports data in .utopia/reports for analytics",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	// Note: The directory detection pattern `.utopia/(\w+)/?` requires the path
	// without trailing slash to avoid the subdirectory check false positive.
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].Category != "directory" {
		t.Errorf("Category = %q, want %q", candidates[0].Category, "directory")
	}
}

func TestStrategy_Detect_MultipleQualifyingFeatures(t *testing.T) {
	s := New()
	documented := &readme.READMEDocumented{
		Commands:    []string{"cr", "execute"},
		Directories: []string{"specs"},
	}

	specs := []*domain.Spec{
		{
			ID: "multi-feature",
			Features: []domain.Feature{
				{
					ID:          "status-command",
					Description: "CLI command to run utopia status",
				},
				{
					ID:          "reports-dir",
					Description: "Store data in .utopia/reports for archiving",
				},
			},
		},
	}

	candidates := s.Detect(specs, documented)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Check we got both categories
	categories := make(map[string]bool)
	for _, c := range candidates {
		categories[c.Category] = true
	}

	if !categories["command"] {
		t.Error("expected command category candidate")
	}
	if !categories["directory"] {
		t.Error("expected directory category candidate")
	}
}

func TestStrategy_ImplementsInterface(t *testing.T) {
	// Compile-time check that Strategy implements readme.Strategy
	var _ readme.Strategy = (*Strategy)(nil)
}
