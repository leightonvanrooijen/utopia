// Package types provides static analysis of type definitions in source files.
// This file adds relationship detection between domain entities.
package types

import (
	"sort"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// RelationshipType defines the kind of relationship between entities
type RelationshipType string

const (
	// RelationshipContains indicates an entity owns/embeds another (embedded fields, nested types)
	RelationshipContains RelationshipType = "contains"
	// RelationshipReferences indicates an entity points to another (field types)
	RelationshipReferences RelationshipType = "references"
	// RelationshipProduces indicates an entity generates another (method return types)
	RelationshipProduces RelationshipType = "produces"
	// RelationshipConsumes indicates an entity uses another as input (method parameters)
	RelationshipConsumes RelationshipType = "consumes"
)

// DiscoveredRelationship represents a relationship found between two domain types
type DiscoveredRelationship struct {
	SourceType string           // The type that has the relationship
	TargetType string           // The type being related to
	Type       RelationshipType // The kind of relationship
	Evidence   RelationshipEvidence
}

// RelationshipEvidence captures where a relationship was discovered
type RelationshipEvidence struct {
	SourceFile string // File where the relationship was found
	LineNumber int    // Line number of the relationship
	FieldName  string // For field-based relationships, the field name
	MethodName string // For method-based relationships, the method name
}

// RelationshipAnalyzer detects relationships between discovered domain types
type RelationshipAnalyzer struct {
	// knownTypes maps type names to their discovered definitions
	// Used to filter relationships to only include known domain types
	knownTypes map[string]*DiscoveredType
}

// NewRelationshipAnalyzer creates a new analyzer for detecting entity relationships
func NewRelationshipAnalyzer() *RelationshipAnalyzer {
	return &RelationshipAnalyzer{
		knownTypes: make(map[string]*DiscoveredType),
	}
}

// AnalyzeRelationships detects relationships between the given discovered types.
// Only relationships between types in the provided slice are included.
func (r *RelationshipAnalyzer) AnalyzeRelationships(types []*DiscoveredType) []*DiscoveredRelationship {
	// Build a map of known types for filtering
	r.knownTypes = make(map[string]*DiscoveredType)
	for _, t := range types {
		r.knownTypes[t.Name] = t
	}

	var relationships []*DiscoveredRelationship

	for _, t := range types {
		// Detect relationships from fields
		fieldRels := r.analyzeFieldRelationships(t)
		relationships = append(relationships, fieldRels...)

		// Detect relationships from methods (for interfaces)
		methodRels := r.analyzeMethodRelationships(t)
		relationships = append(relationships, methodRels...)
	}

	// Deduplicate and sort relationships
	relationships = r.deduplicateRelationships(relationships)

	return relationships
}

// analyzeFieldRelationships detects relationships from a type's fields
func (r *RelationshipAnalyzer) analyzeFieldRelationships(t *DiscoveredType) []*DiscoveredRelationship {
	var relationships []*DiscoveredRelationship

	for _, field := range t.Fields {
		// Skip if field type is empty or not a known domain type
		if field.Type == "" {
			continue
		}

		targetType := field.Type

		// Skip if target is not a known domain type
		if _, known := r.knownTypes[targetType]; !known {
			continue
		}

		// Skip self-references
		if targetType == t.Name {
			continue
		}

		// Determine relationship type
		relType := RelationshipReferences
		if field.IsEmbedded {
			relType = RelationshipContains
		}

		relationships = append(relationships, &DiscoveredRelationship{
			SourceType: t.Name,
			TargetType: targetType,
			Type:       relType,
			Evidence: RelationshipEvidence{
				SourceFile: t.FilePath,
				LineNumber: field.LineNumber,
				FieldName:  field.Name,
			},
		})
	}

	return relationships
}

// analyzeMethodRelationships detects relationships from a type's method signatures
func (r *RelationshipAnalyzer) analyzeMethodRelationships(t *DiscoveredType) []*DiscoveredRelationship {
	var relationships []*DiscoveredRelationship

	for _, method := range t.Methods {
		// Analyze parameter types -> "consumes" relationships
		for _, paramType := range method.Parameters {
			if _, known := r.knownTypes[paramType]; !known {
				continue
			}
			if paramType == t.Name {
				continue
			}

			relationships = append(relationships, &DiscoveredRelationship{
				SourceType: t.Name,
				TargetType: paramType,
				Type:       RelationshipConsumes,
				Evidence: RelationshipEvidence{
					SourceFile: t.FilePath,
					LineNumber: method.LineNumber,
					MethodName: method.Name,
				},
			})
		}

		// Analyze return types -> "produces" relationships
		for _, returnType := range method.ReturnTypes {
			if _, known := r.knownTypes[returnType]; !known {
				continue
			}
			if returnType == t.Name {
				continue
			}

			relationships = append(relationships, &DiscoveredRelationship{
				SourceType: t.Name,
				TargetType: returnType,
				Type:       RelationshipProduces,
				Evidence: RelationshipEvidence{
					SourceFile: t.FilePath,
					LineNumber: method.LineNumber,
					MethodName: method.Name,
				},
			})
		}
	}

	return relationships
}

// deduplicateRelationships removes duplicate relationships and sorts the result
func (r *RelationshipAnalyzer) deduplicateRelationships(relationships []*DiscoveredRelationship) []*DiscoveredRelationship {
	// Use a map to track unique relationships (source + target + type)
	seen := make(map[string]*DiscoveredRelationship)

	for _, rel := range relationships {
		key := rel.SourceType + "->" + string(rel.Type) + "->" + rel.TargetType
		// Keep the first occurrence (has the evidence)
		if _, exists := seen[key]; !exists {
			seen[key] = rel
		}
	}

	// Convert back to slice
	var result []*DiscoveredRelationship
	for _, rel := range seen {
		result = append(result, rel)
	}

	// Sort for consistent output: by source type, then relationship type, then target
	sort.Slice(result, func(i, j int) bool {
		if result[i].SourceType != result[j].SourceType {
			return result[i].SourceType < result[j].SourceType
		}
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].TargetType < result[j].TargetType
	})

	return result
}

// GroupRelationshipsBySource groups relationships by their source type
func (r *RelationshipAnalyzer) GroupRelationshipsBySource(relationships []*DiscoveredRelationship) map[string][]*DiscoveredRelationship {
	result := make(map[string][]*DiscoveredRelationship)

	for _, rel := range relationships {
		result[rel.SourceType] = append(result[rel.SourceType], rel)
	}

	return result
}

// ToDomainRelationships converts discovered relationships to domain.EntityRelationship format
// for use in draft domain documents
func (r *RelationshipAnalyzer) ToDomainRelationships(relationships []*DiscoveredRelationship) []domain.EntityRelationship {
	var result []domain.EntityRelationship

	for _, rel := range relationships {
		result = append(result, domain.EntityRelationship{
			Type:   string(rel.Type),
			Target: rel.TargetType,
		})
	}

	return result
}

// ToDomainEntitiesWithRelationships creates domain.DomainEntity objects from discovered types
// with their relationships populated
func (r *RelationshipAnalyzer) ToDomainEntitiesWithRelationships(
	types []*DiscoveredType,
	relationships []*DiscoveredRelationship,
) []domain.DomainEntity {
	// Group relationships by source type
	relsBySource := r.GroupRelationshipsBySource(relationships)

	var entities []domain.DomainEntity

	for _, t := range types {
		entity := domain.DomainEntity{
			Name: t.Name,
		}

		// Add relationships for this type
		if rels, ok := relsBySource[t.Name]; ok {
			entity.Relationships = r.ToDomainRelationships(rels)
		}

		entities = append(entities, entity)
	}

	// Sort entities by name for consistent output
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Name < entities[j].Name
	})

	return entities
}

// FilterRelationshipsByContext filters relationships to only include those where
// both source and target types belong to the given bounded context
func (r *RelationshipAnalyzer) FilterRelationshipsByContext(
	relationships []*DiscoveredRelationship,
	contextTypes map[string]bool,
) []*DiscoveredRelationship {
	var filtered []*DiscoveredRelationship

	for _, rel := range relationships {
		// Both source and target must be in the context
		if contextTypes[rel.SourceType] && contextTypes[rel.TargetType] {
			filtered = append(filtered, rel)
		}
	}

	return filtered
}
