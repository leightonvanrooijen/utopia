// Package types provides static analysis of type definitions in source files.
// This file adds schema analysis for database migrations and schema definitions.
package types

import (
	"bufio"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SchemaSource indicates where a schema entity was discovered
type SchemaSource string

const (
	// SchemaSourceSQL indicates the entity was found in a SQL migration or schema file
	SchemaSourceSQL SchemaSource = "sql"
	// SchemaSourcePrisma indicates the entity was found in a Prisma schema file
	SchemaSourcePrisma SchemaSource = "prisma"
)

// SchemaEntity represents a table/model discovered from database schema
type SchemaEntity struct {
	Name        string             // Table/model name (converted to PascalCase for domain matching)
	RawName     string             // Original name as it appears in schema (e.g., snake_case)
	Source      SchemaSource       // Where this was discovered
	FilePath    string             // Path to the schema file
	LineNumber  int                // Line where the entity is defined
	Columns     []SchemaColumn     // Columns/fields of this entity
	ForeignKeys []SchemaForeignKey // Foreign key relationships
}

// SchemaColumn represents a column/field within a schema entity
type SchemaColumn struct {
	Name       string // Column name (converted to PascalCase)
	RawName    string // Original name as it appears in schema
	Type       string // Column type (e.g., "VARCHAR", "INTEGER", "String")
	LineNumber int
	IsPrimary  bool // True if this is a primary key
	IsNullable bool // True if the column can be NULL
}

// SchemaForeignKey represents a foreign key relationship
type SchemaForeignKey struct {
	ColumnName     string // The column with the foreign key (PascalCase)
	RawColumnName  string // Original column name
	TargetTable    string // Referenced table (PascalCase)
	RawTargetTable string // Original table name
	TargetColumn   string // Referenced column (typically primary key)
	LineNumber     int
}

// SchemaAnalyzer extracts domain entities from database schemas and migrations
type SchemaAnalyzer struct {
	// SQL patterns
	createTableRegex      *regexp.Regexp
	columnDefRegex        *regexp.Regexp
	primaryKeyRegex       *regexp.Regexp
	foreignKeyRegex       *regexp.Regexp
	inlineForeignKeyRegex *regexp.Regexp
	alterTableFKRegex     *regexp.Regexp

	// Prisma patterns
	prismaModelRegex    *regexp.Regexp
	prismaFieldRegex    *regexp.Regexp
	prismaRelationRegex *regexp.Regexp
}

// NewSchemaAnalyzer creates a new schema analyzer
func NewSchemaAnalyzer() *SchemaAnalyzer {
	return &SchemaAnalyzer{
		// SQL CREATE TABLE pattern
		createTableRegex: regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?\s*\(`),

		// SQL column definition pattern (captures name and type)
		// Case-insensitive matching for column types
		// Types are ordered longest-first to avoid partial matches (e.g., INTEGER before INT)
		columnDefRegex: regexp.MustCompile(`(?i)^\s*["\x60]?(\w+)["\x60]?\s+((?:TIMESTAMPTZ|TIMESTAMP|DATETIME|BIGSERIAL|SMALLSERIAL|SERIAL|BIGINT|SMALLINT|TINYINT|INTEGER|INT|VARCHAR|CHAR|TEXT|DECIMAL|NUMERIC|DOUBLE|FLOAT|REAL|BOOLEAN|BOOL|DATE|TIME|BLOB|UUID|BYTEA|JSONB|JSON|ARRAY|MONEY|INTERVAL|POINT|LINE|CIDR|INET|MACADDR)(?:\([^)]+\))?)`),

		// SQL PRIMARY KEY pattern (inline)
		primaryKeyRegex: regexp.MustCompile(`(?i)PRIMARY\s+KEY`),

		// SQL FOREIGN KEY constraint pattern (table-level)
		foreignKeyRegex: regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\(["\x60]?(\w+)["\x60]?\)\s*REFERENCES\s+["\x60]?(\w+)["\x60]?\s*\(["\x60]?(\w+)["\x60]?\)`),

		// SQL inline REFERENCES pattern
		inlineForeignKeyRegex: regexp.MustCompile(`(?i)REFERENCES\s+["\x60]?(\w+)["\x60]?\s*\(["\x60]?(\w+)["\x60]?\)`),

		// SQL ALTER TABLE ADD FOREIGN KEY pattern
		alterTableFKRegex: regexp.MustCompile(`(?i)ALTER\s+TABLE\s+["\x60]?(\w+)["\x60]?\s+ADD\s+(?:CONSTRAINT\s+\w+\s+)?FOREIGN\s+KEY\s*\(["\x60]?(\w+)["\x60]?\)\s*REFERENCES\s+["\x60]?(\w+)["\x60]?\s*\(["\x60]?(\w+)["\x60]?\)`),

		// Prisma model definition
		prismaModelRegex: regexp.MustCompile(`^model\s+(\w+)\s*\{`),

		// Prisma field definition (name, type, optional modifiers)
		prismaFieldRegex: regexp.MustCompile(`^\s+(\w+)\s+(\w+)(\[\])?\s*(\?)?`),

		// Prisma @relation attribute
		prismaRelationRegex: regexp.MustCompile(`@relation\s*\([^)]*fields:\s*\[(\w+)\][^)]*references:\s*\[(\w+)\]`),
	}
}

// AnalyzeSQLFile extracts entities from a SQL schema or migration file
func (s *SchemaAnalyzer) AnalyzeSQLFile(filePath, content string) []*SchemaEntity {
	var entities []*SchemaEntity
	var currentEntity *SchemaEntity
	var currentTableContent strings.Builder
	parenDepth := 0

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmedLine, "--") || strings.HasPrefix(trimmedLine, "/*") || trimmedLine == "" {
			continue
		}

		// Check for CREATE TABLE
		if matches := s.createTableRegex.FindStringSubmatch(line); matches != nil {
			tableName := matches[1]
			currentEntity = &SchemaEntity{
				Name:       toPascalCase(tableName),
				RawName:    tableName,
				Source:     SchemaSourceSQL,
				FilePath:   filePath,
				LineNumber: lineNum,
			}
			currentTableContent.Reset()
			parenDepth = strings.Count(line, "(") - strings.Count(line, ")")
			currentTableContent.WriteString(line)
			continue
		}

		// Track content within CREATE TABLE
		if currentEntity != nil {
			currentTableContent.WriteString("\n")
			currentTableContent.WriteString(line)
			parenDepth += strings.Count(line, "(") - strings.Count(line, ")")

			// Extract columns
			if col := s.extractSQLColumn(line, lineNum); col != nil {
				currentEntity.Columns = append(currentEntity.Columns, *col)

				// Check for inline foreign key reference
				if matches := s.inlineForeignKeyRegex.FindStringSubmatch(line); matches != nil {
					currentEntity.ForeignKeys = append(currentEntity.ForeignKeys, SchemaForeignKey{
						ColumnName:     col.Name,
						RawColumnName:  col.RawName,
						TargetTable:    toPascalCase(matches[1]),
						RawTargetTable: matches[1],
						TargetColumn:   matches[2],
						LineNumber:     lineNum,
					})
				}
			}

			// Check for table-level FOREIGN KEY constraint
			if matches := s.foreignKeyRegex.FindStringSubmatch(line); matches != nil {
				currentEntity.ForeignKeys = append(currentEntity.ForeignKeys, SchemaForeignKey{
					ColumnName:     toPascalCase(matches[1]),
					RawColumnName:  matches[1],
					TargetTable:    toPascalCase(matches[2]),
					RawTargetTable: matches[2],
					TargetColumn:   matches[3],
					LineNumber:     lineNum,
				})
			}

			// Table definition complete
			if parenDepth <= 0 {
				entities = append(entities, currentEntity)
				currentEntity = nil
			}
		}

		// Check for ALTER TABLE ADD FOREIGN KEY (outside CREATE TABLE)
		if currentEntity == nil {
			if matches := s.alterTableFKRegex.FindStringSubmatch(line); matches != nil {
				tableName := toPascalCase(matches[1])
				// Find existing entity or create placeholder
				var targetEntity *SchemaEntity
				for _, e := range entities {
					if e.Name == tableName {
						targetEntity = e
						break
					}
				}
				if targetEntity != nil {
					targetEntity.ForeignKeys = append(targetEntity.ForeignKeys, SchemaForeignKey{
						ColumnName:     toPascalCase(matches[2]),
						RawColumnName:  matches[2],
						TargetTable:    toPascalCase(matches[3]),
						RawTargetTable: matches[3],
						TargetColumn:   matches[4],
						LineNumber:     lineNum,
					})
				}
			}
		}
	}

	return entities
}

// extractSQLColumn extracts column information from a line within CREATE TABLE
func (s *SchemaAnalyzer) extractSQLColumn(line string, lineNum int) *SchemaColumn {
	trimmed := strings.TrimSpace(line)

	// Skip constraint definitions
	lowerTrimmed := strings.ToLower(trimmed)
	if strings.HasPrefix(lowerTrimmed, "primary key") ||
		strings.HasPrefix(lowerTrimmed, "foreign key") ||
		strings.HasPrefix(lowerTrimmed, "unique") ||
		strings.HasPrefix(lowerTrimmed, "check") ||
		strings.HasPrefix(lowerTrimmed, "constraint") ||
		strings.HasPrefix(lowerTrimmed, "index") ||
		strings.HasPrefix(lowerTrimmed, ")") {
		return nil
	}

	if matches := s.columnDefRegex.FindStringSubmatch(trimmed); matches != nil {
		colName := matches[1]
		colType := strings.ToUpper(matches[2])

		return &SchemaColumn{
			Name:       toPascalCase(colName),
			RawName:    colName,
			Type:       colType,
			LineNumber: lineNum,
			IsPrimary:  s.primaryKeyRegex.MatchString(line),
			IsNullable: !strings.Contains(strings.ToUpper(line), "NOT NULL"),
		}
	}

	return nil
}

// AnalyzePrismaFile extracts entities from a Prisma schema file
func (s *SchemaAnalyzer) AnalyzePrismaFile(filePath, content string) []*SchemaEntity {
	var entities []*SchemaEntity
	var currentEntity *SchemaEntity
	braceDepth := 0

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmedLine, "//") || trimmedLine == "" {
			continue
		}

		// Check for model definition
		if matches := s.prismaModelRegex.FindStringSubmatch(trimmedLine); matches != nil {
			modelName := matches[1]
			currentEntity = &SchemaEntity{
				Name:       modelName, // Prisma models are already PascalCase
				RawName:    modelName,
				Source:     SchemaSourcePrisma,
				FilePath:   filePath,
				LineNumber: lineNum,
			}
			braceDepth = 1
			continue
		}

		// Track content within model
		if currentEntity != nil && braceDepth > 0 {
			braceDepth += strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}")

			// Extract field
			if matches := s.prismaFieldRegex.FindStringSubmatch(line); matches != nil {
				fieldName := matches[1]
				fieldType := matches[2]
				isArray := matches[3] == "[]"
				isOptional := matches[4] == "?"

				// Skip internal Prisma fields
				if fieldName == "id" || strings.HasPrefix(fieldName, "@@") || strings.HasPrefix(fieldName, "@") {
					// Still add id as a column
					if fieldName == "id" {
						currentEntity.Columns = append(currentEntity.Columns, SchemaColumn{
							Name:       toPascalCase(fieldName),
							RawName:    fieldName,
							Type:       fieldType,
							LineNumber: lineNum,
							IsPrimary:  true,
							IsNullable: false,
						})
					}
				} else if !strings.HasPrefix(fieldName, "@") {
					col := SchemaColumn{
						Name:       toPascalCase(fieldName),
						RawName:    fieldName,
						Type:       fieldType,
						LineNumber: lineNum,
						IsPrimary:  false,
						IsNullable: isOptional,
					}
					currentEntity.Columns = append(currentEntity.Columns, col)

					// Check for @relation to detect foreign keys
					if relMatches := s.prismaRelationRegex.FindStringSubmatch(line); relMatches != nil {
						fkColumn := relMatches[1]
						targetColumn := relMatches[2]
						currentEntity.ForeignKeys = append(currentEntity.ForeignKeys, SchemaForeignKey{
							ColumnName:     toPascalCase(fkColumn),
							RawColumnName:  fkColumn,
							TargetTable:    fieldType, // In Prisma, the field type is the related model
							RawTargetTable: fieldType,
							TargetColumn:   targetColumn,
							LineNumber:     lineNum,
						})
					} else if !isArray && isUpperCaseStart(fieldType) && !isPrismaScalarType(fieldType) {
						// Relation field without explicit @relation (implicit relation)
						// The foreign key column is typically fieldName + "Id"
						fkColumnName := fieldName + "Id"
						currentEntity.ForeignKeys = append(currentEntity.ForeignKeys, SchemaForeignKey{
							ColumnName:     toPascalCase(fkColumnName),
							RawColumnName:  fkColumnName,
							TargetTable:    fieldType,
							RawTargetTable: fieldType,
							TargetColumn:   "id",
							LineNumber:     lineNum,
						})
					}
				}
			}

			// Model definition complete
			if braceDepth == 0 {
				entities = append(entities, currentEntity)
				currentEntity = nil
			}
		}
	}

	return entities
}

// isPrismaScalarType returns true if the type is a Prisma scalar type
func isPrismaScalarType(t string) bool {
	scalarTypes := map[string]bool{
		"String":   true,
		"Boolean":  true,
		"Int":      true,
		"BigInt":   true,
		"Float":    true,
		"Decimal":  true,
		"DateTime": true,
		"Json":     true,
		"Bytes":    true,
	}
	return scalarTypes[t]
}

// isUpperCaseStart returns true if the string starts with an uppercase letter
func isUpperCaseStart(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// toPascalCase converts snake_case or kebab-case to PascalCase.
// For camelCase input (like Prisma fields), it just capitalizes the first letter.
func toPascalCase(s string) string {
	// Handle empty string
	if s == "" {
		return s
	}

	// Check if this is camelCase (no underscores/hyphens)
	// In this case, just capitalize the first letter and preserve the rest
	if !strings.Contains(s, "_") && !strings.Contains(s, "-") {
		return strings.ToUpper(string(s[0])) + s[1:]
	}

	// Split on underscores and hyphens for snake_case/kebab-case
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})

	var result strings.Builder
	for _, part := range parts {
		if len(part) > 0 {
			result.WriteString(strings.ToUpper(string(part[0])))
			if len(part) > 1 {
				result.WriteString(strings.ToLower(part[1:]))
			}
		}
	}

	return result.String()
}

// ToDiscoveredTypes converts schema entities to DiscoveredType format for integration
// with the existing type analysis system
func (s *SchemaAnalyzer) ToDiscoveredTypes(entities []*SchemaEntity) []*DiscoveredType {
	var types []*DiscoveredType

	for _, entity := range entities {
		t := &DiscoveredType{
			Name:       entity.Name,
			Kind:       "schema",
			FilePath:   entity.FilePath,
			LineNumber: entity.LineNumber,
			Language:   string(entity.Source),
		}

		// Convert columns to fields
		for _, col := range entity.Columns {
			t.Fields = append(t.Fields, DiscoveredField{
				Name:       col.Name,
				Type:       col.Type,
				LineNumber: col.LineNumber,
				IsEmbedded: false,
			})
		}

		types = append(types, t)
	}

	return types
}

// ToDiscoveredRelationships converts schema foreign keys to DiscoveredRelationship format
func (s *SchemaAnalyzer) ToDiscoveredRelationships(entities []*SchemaEntity) []*DiscoveredRelationship {
	// Build a map of known entity names for filtering
	knownEntities := make(map[string]bool)
	for _, e := range entities {
		knownEntities[e.Name] = true
	}

	var relationships []*DiscoveredRelationship

	for _, entity := range entities {
		for _, fk := range entity.ForeignKeys {
			// Only include relationships to known entities
			if !knownEntities[fk.TargetTable] {
				continue
			}

			relationships = append(relationships, &DiscoveredRelationship{
				SourceType: entity.Name,
				TargetType: fk.TargetTable,
				Type:       RelationshipReferences,
				Evidence: RelationshipEvidence{
					SourceFile: entity.FilePath,
					LineNumber: fk.LineNumber,
					FieldName:  fk.ColumnName,
				},
			})
		}
	}

	// Deduplicate and sort
	seen := make(map[string]*DiscoveredRelationship)
	for _, rel := range relationships {
		key := rel.SourceType + "->" + string(rel.Type) + "->" + rel.TargetType
		if _, exists := seen[key]; !exists {
			seen[key] = rel
		}
	}

	var result []*DiscoveredRelationship
	for _, rel := range seen {
		result = append(result, rel)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].SourceType != result[j].SourceType {
			return result[i].SourceType < result[j].SourceType
		}
		return result[i].TargetType < result[j].TargetType
	})

	return result
}

// SchemaFilePaths returns common schema file path patterns to scan
func SchemaFilePaths() []string {
	return []string{
		"migrations/*.sql",
		"migrations/**/*.sql",
		"db/migrations/*.sql",
		"db/migrations/**/*.sql",
		"database/migrations/*.sql",
		"database/migrations/**/*.sql",
		"schema.sql",
		"db/schema.sql",
		"database/schema.sql",
		"schema.prisma",
		"prisma/schema.prisma",
	}
}

// IsSchemaFile returns true if the file path matches common schema file patterns
func IsSchemaFile(filePath string) bool {
	base := filepath.Base(filePath)
	ext := filepath.Ext(filePath)

	// Direct schema files
	if base == "schema.sql" || base == "schema.prisma" {
		return true
	}

	// Migration files
	dir := filepath.Dir(filePath)
	if strings.Contains(dir, "migration") && ext == ".sql" {
		return true
	}

	// Prisma files
	if ext == ".prisma" {
		return true
	}

	return false
}

// MergeSchemaAndTypeTerms merges schema-discovered terms with type-discovered terms,
// boosting confidence for terms that appear in both sources
func MergeSchemaAndTypeTerms(
	typeTerms map[string]*TermOccurrence,
	schemaEntities []*SchemaEntity,
) map[string]*TermOccurrence {
	// Create a copy of typeTerms to avoid modifying the original
	merged := make(map[string]*TermOccurrence)
	for k, v := range typeTerms {
		merged[k] = v
	}

	// Process schema entities
	for _, entity := range schemaEntities {
		term := entity.Name

		if existing, exists := merged[term]; exists {
			// Term exists in both sources - boost confidence
			if existing.Confidence != TermConfidenceHigh {
				existing.Confidence = TermConfidenceHigh
			}
			// Add schema file to evidence
			if !containsString(existing.Files, entity.FilePath) {
				existing.Files = append(existing.Files, entity.FilePath)
			}
			lineRef := formatLineReference(entity.FilePath, entity.LineNumber)
			existing.Lines = append(existing.Lines, lineRef)
		} else {
			// New term from schema only
			merged[term] = &TermOccurrence{
				Term:       term,
				Files:      []string{entity.FilePath},
				Lines:      []string{formatLineReference(entity.FilePath, entity.LineNumber)},
				Confidence: TermConfidenceMedium, // Schema-only terms start at medium
				Types:      nil,                  // No code types for this term
			}
		}

		// Also track columns as potential terms
		for _, col := range entity.Columns {
			colTerm := col.Name
			if _, exists := merged[colTerm]; !exists {
				merged[colTerm] = &TermOccurrence{
					Term:       colTerm,
					Files:      []string{entity.FilePath},
					Lines:      []string{formatLineReference(entity.FilePath, col.LineNumber)},
					Confidence: TermConfidenceLow, // Columns alone are low confidence
					Types:      nil,
				}
			} else {
				// Column name also appears in code - boost to medium if low
				existing := merged[colTerm]
				if existing.Confidence == TermConfidenceLow {
					existing.Confidence = TermConfidenceMedium
				}
			}
		}
	}

	return merged
}
