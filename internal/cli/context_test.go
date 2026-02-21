package cli

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestBuildDomainContext_Empty(t *testing.T) {
	result := buildDomainContext([]*domain.DomainDoc{})

	if result != "" {
		t.Error("buildDomainContext should return empty string for empty docs")
	}
}

func TestBuildDomainContext_WithTerms(t *testing.T) {
	docs := []*domain.DomainDoc{
		{
			ID:    "specs",
			Title: "Specification System",
			Terms: []domain.DomainTerm{
				{
					Term:       "Spec",
					Definition: "A specification document with ID, Title, Description",
					Aliases:    []string{"specification", "spec document"},
				},
				{
					Term:       "Feature",
					Definition: "A capability within a spec",
					Aliases:    []string{"capability"},
				},
			},
		},
	}

	result := buildDomainContext(docs)

	// Should contain header
	if !strings.Contains(result, "## Domain Language Guide") {
		t.Error("result should contain Domain Language Guide header")
	}

	// Should contain title
	if !strings.Contains(result, "### Specification System") {
		t.Error("result should contain bounded context title")
	}

	// Should contain term guidance with aliases
	if !strings.Contains(result, "Use **Spec**") {
		t.Error("result should contain term name in bold")
	}
	if !strings.Contains(result, "\"specification\"") {
		t.Error("result should contain aliases in quotes")
	}

	// Should contain definition
	if !strings.Contains(result, "Meaning:") {
		t.Error("result should contain term meaning")
	}
}

func TestBuildDomainContext_WithEntities(t *testing.T) {
	docs := []*domain.DomainDoc{
		{
			ID:    "specs",
			Title: "Specification System",
			Entities: []domain.DomainEntity{
				{
					Name: "Spec",
					Relationships: []domain.EntityRelationship{
						{Type: "contains", Target: "Feature"},
						{Type: "updated-by", Target: "ChangeRequest"},
					},
				},
			},
		},
	}

	result := buildDomainContext(docs)

	// Should contain entity relationships section
	if !strings.Contains(result, "### Entity Relationships") {
		t.Error("result should contain Entity Relationships section")
	}

	// Should contain relationships
	if !strings.Contains(result, "Spec contains Feature") {
		t.Error("result should contain 'Spec contains Feature' relationship")
	}
	if !strings.Contains(result, "Spec updated-by ChangeRequest") {
		t.Error("result should contain 'Spec updated-by ChangeRequest' relationship")
	}
}

func TestBuildDomainContext_NoEntitiesSection_WhenNoRelationships(t *testing.T) {
	docs := []*domain.DomainDoc{
		{
			ID:    "specs",
			Title: "Specification System",
			Terms: []domain.DomainTerm{
				{
					Term:       "Spec",
					Definition: "A specification document",
				},
			},
			Entities: []domain.DomainEntity{
				{
					Name:          "Spec",
					Relationships: []domain.EntityRelationship{}, // No relationships
				},
			},
		},
	}

	result := buildDomainContext(docs)

	// Should NOT contain entity relationships section when entities have no relationships
	if strings.Contains(result, "### Entity Relationships") {
		t.Error("result should NOT contain Entity Relationships section when no relationships exist")
	}
}

func TestFormatAliases(t *testing.T) {
	tests := []struct {
		name     string
		aliases  []string
		expected string
	}{
		{
			name:     "empty",
			aliases:  []string{},
			expected: "",
		},
		{
			name:     "single",
			aliases:  []string{"alternative"},
			expected: "\"alternative\"",
		},
		{
			name:     "multiple",
			aliases:  []string{"first", "second", "third"},
			expected: "\"first\", \"second\", \"third\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatAliases(tt.aliases)
			if result != tt.expected {
				t.Errorf("formatAliases(%v) = %q, want %q", tt.aliases, result, tt.expected)
			}
		})
	}
}

func TestBuildDomainContext_TruncatesLongDefinitions(t *testing.T) {
	// Create a definition longer than 150 characters
	longDefinition := strings.Repeat("This is a long definition. ", 10)

	docs := []*domain.DomainDoc{
		{
			ID:    "test",
			Title: "Test Context",
			Terms: []domain.DomainTerm{
				{
					Term:       "LongTerm",
					Definition: longDefinition,
				},
			},
		},
	}

	result := buildDomainContext(docs)

	// Should truncate with ellipsis
	if !strings.Contains(result, "...") {
		t.Error("result should truncate long definitions with ellipsis")
	}
}
