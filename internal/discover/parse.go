package discover

import (
	"fmt"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"gopkg.in/yaml.v3"
)

// YAML parsing types for spec discovery
type draftsOutput struct {
	Drafts []draftOutput `yaml:"drafts"`
}
type draftOutput struct {
	ID               string          `yaml:"id"`
	Title            string          `yaml:"title"`
	Description      string          `yaml:"description"`
	Confidence       string          `yaml:"confidence"`
	DiscoveredFrom   []string        `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string        `yaml:"uncertainty_notes,omitempty"`
	Evidence         evidenceOutput  `yaml:"evidence"`
	Features         []featureOutput `yaml:"features"`
	DomainKnowledge  []string        `yaml:"domain_knowledge,omitempty"`
}
type evidenceOutput struct {
	CodeFiles []string `yaml:"code_files,omitempty"`
	TestFiles []string `yaml:"test_files,omitempty"`
	DocFiles  []string `yaml:"doc_files,omitempty"`
	Comments  []string `yaml:"comments,omitempty"`
}
type featureOutput struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}
type qualificationOutput struct {
	Qualified    []qualifiedCandidate    `yaml:"qualified"`
	Disqualified []disqualifiedCandidate `yaml:"disqualified"`
}
type qualifiedCandidate struct {
	ID                  string   `yaml:"id"`
	Title               string   `yaml:"title"`
	Description         string   `yaml:"description"`
	SourceFiles         []string `yaml:"source_files,omitempty"`
	QualificationReason string   `yaml:"qualification_reason"`
}
type disqualifiedCandidate struct {
	ID     string `yaml:"id"`
	Reason string `yaml:"reason"`
}

func parseQualifiedCandidates(output string) []qualifiedCandidate {
	yamlContent := internal.ExtractYAMLBlock(output)
	if yamlContent == "" {
		return nil
	}
	var qOutput qualificationOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &qOutput); err != nil {
		return nil
	}
	return qOutput.Qualified
}

func parseDisqualifiedCandidates(output string) []disqualifiedCandidate {
	yamlContent := internal.ExtractYAMLBlock(output)
	if yamlContent == "" {
		return nil
	}
	var qOutput qualificationOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &qOutput); err != nil {
		return nil
	}
	return qOutput.Disqualified
}

func parseSingleDraftFromOutput(output string) (*domain.DraftSpec, error) {
	yamlContent := internal.ExtractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}
	var singleDraft struct {
		Draft draftOutput `yaml:"draft"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &singleDraft); err == nil && singleDraft.Draft.ID != "" {
		return convertDraftOutput(singleDraft.Draft), nil
	}
	var draftsOut draftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	if len(draftsOut.Drafts) == 0 {
		return nil, fmt.Errorf("no drafts found in output")
	}
	return convertDraftOutput(draftsOut.Drafts[0]), nil
}

func convertDraftOutput(d draftOutput) *domain.DraftSpec {
	confidence := domain.DraftConfidenceMedium
	switch strings.ToLower(d.Confidence) {
	case "high":
		confidence = domain.DraftConfidenceHigh
	case "low":
		confidence = domain.DraftConfidenceLow
	}
	draft := &domain.DraftSpec{
		ID: d.ID, Title: d.Title, Created: time.Now(), Description: d.Description,
		Confidence: confidence, DiscoveredFrom: d.DiscoveredFrom, UncertaintyNotes: d.UncertaintyNotes,
		Evidence:        domain.DraftEvidence{CodeFiles: d.Evidence.CodeFiles, TestFiles: d.Evidence.TestFiles, DocFiles: d.Evidence.DocFiles, Comments: d.Evidence.Comments},
		DomainKnowledge: d.DomainKnowledge,
	}
	for _, f := range d.Features {
		draft.Features = append(draft.Features, domain.Feature{ID: f.ID, Description: f.Description, AcceptanceCriteria: f.AcceptanceCriteria})
	}
	return draft
}

func countYAMLItems(yamlOutput, key string) int {
	yamlContent := internal.ExtractYAMLBlock(yamlOutput)
	if yamlContent == "" {
		return 0
	}
	var data map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &data); err != nil {
		return 0
	}
	if items, ok := data[key]; ok {
		if list, ok := items.([]interface{}); ok {
			return len(list)
		}
	}
	return 0
}

// YAML parsing types for domain discovery
type domainDraftsOutput struct {
	Drafts []domainDraftOutput `yaml:"drafts"`
}
type domainDraftOutput struct {
	ID               string               `yaml:"id"`
	Title            string               `yaml:"title"`
	BoundedContext   string               `yaml:"bounded_context"`
	Description      string               `yaml:"description"`
	Confidence       string               `yaml:"confidence"`
	DiscoveredFrom   []string             `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string             `yaml:"uncertainty_notes,omitempty"`
	Evidence         domainEvidenceOutput `yaml:"evidence"`
	Terms            []domainTermOutput   `yaml:"terms,omitempty"`
	Entities         []domainEntityOutput `yaml:"entities,omitempty"`
}
type domainEvidenceOutput struct {
	TypeFiles    []string `yaml:"type_files,omitempty"`
	PackageFiles []string `yaml:"package_files,omitempty"`
	SchemaFiles  []string `yaml:"schema_files,omitempty"`
	Comments     []string `yaml:"comments,omitempty"`
}
type domainTermOutput struct {
	Term             string                    `yaml:"term"`
	Canonical        bool                      `yaml:"canonical"`
	CodeUsage        string                    `yaml:"code_usage"`
	Definition       string                    `yaml:"definition"`
	Aliases          []string                  `yaml:"aliases,omitempty"`
	CrossContextNote string                    `yaml:"cross_context_note,omitempty"`
	Evidence         *domainTermEvidenceOutput `yaml:"evidence,omitempty"`
}
type domainTermEvidenceOutput struct {
	Files []string `yaml:"files,omitempty"`
	Lines []string `yaml:"lines,omitempty"`
}
type domainEntityOutput struct {
	Name          string                     `yaml:"name"`
	Description   string                     `yaml:"description,omitempty"`
	Relationships []domainRelationshipOutput `yaml:"relationships,omitempty"`
}
type domainRelationshipOutput struct {
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
}

func parseDomainDraftsFromOutput(output string) ([]*domain.DraftDomainDoc, error) {
	yamlContent := internal.ExtractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}
	var draftsOut domainDraftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var drafts []*domain.DraftDomainDoc
	now := time.Now()
	for _, d := range draftsOut.Drafts {
		confidence := domain.DraftDomainConfidenceMedium
		switch strings.ToLower(d.Confidence) {
		case "high":
			confidence = domain.DraftDomainConfidenceHigh
		case "low":
			confidence = domain.DraftDomainConfidenceLow
		}
		draft := &domain.DraftDomainDoc{
			ID: d.ID, Title: d.Title, BoundedContext: d.BoundedContext, Description: d.Description,
			Confidence: confidence, Created: now, DiscoveredFrom: d.DiscoveredFrom, UncertaintyNotes: d.UncertaintyNotes,
			Evidence: domain.DraftDomainEvidence{TypeFiles: d.Evidence.TypeFiles, PackageFiles: d.Evidence.PackageFiles, SchemaFiles: d.Evidence.SchemaFiles, Comments: d.Evidence.Comments},
		}
		for _, t := range d.Terms {
			term := domain.DomainTerm{Term: t.Term, Canonical: t.Canonical, CodeUsage: t.CodeUsage, Definition: t.Definition, Aliases: t.Aliases, CrossContextNote: t.CrossContextNote}
			if t.Evidence != nil && (len(t.Evidence.Files) > 0 || len(t.Evidence.Lines) > 0) {
				term.Evidence = &domain.TermEvidence{Files: t.Evidence.Files, Lines: t.Evidence.Lines}
			}
			draft.Terms = append(draft.Terms, term)
		}
		for _, e := range d.Entities {
			entity := domain.DomainEntity{Name: e.Name, Description: e.Description}
			for _, r := range e.Relationships {
				entity.Relationships = append(entity.Relationships, domain.EntityRelationship{Type: r.Type, Target: r.Target})
			}
			draft.Entities = append(draft.Entities, entity)
		}
		drafts = append(drafts, draft)
	}
	return drafts, nil
}
