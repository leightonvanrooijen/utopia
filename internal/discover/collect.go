package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/analysis/types"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
)

// File collection types
type collectedFile struct {
	path    string
	content string
	modTime time.Time
}
type skippedFile struct {
	path   string
	reason string
}

func collectCodebaseContext(projectDir string, scope Scope, progress *ui.Progress) (string, []string, error) {
	var sb strings.Builder
	var filesAnalyzed []string

	searchRoots := scope.Paths
	if len(searchRoots) == 0 {
		searchRoots = []string{projectDir}
	} else {
		absoluteRoots := make([]string, 0, len(searchRoots))
		for _, p := range searchRoots {
			if filepath.IsAbs(p) {
				absoluteRoots = append(absoluteRoots, p)
			} else {
				absoluteRoots = append(absoluteRoots, filepath.Join(projectDir, p))
			}
		}
		searchRoots = absoluteRoots
	}

	var allFiles []collectedFile
	const maxTotalSize int64 = 200000
	for _, root := range searchRoots {
		files, skipped, err := collectAllTextFiles(root, projectDir, maxTotalSize, scope.ExcludePatterns, progress)
		if err != nil {
			continue
		}
		allFiles = append(allFiles, files...)
		for _, skip := range skipped {
			progress.Verbosef("\n  Skipped: %s (%s)", skip.path, skip.reason)
		}
	}

	if len(allFiles) > 0 {
		sb.WriteString("\n### Source Files\n\n")
		for _, f := range allFiles {
			progress.Verbosef("\n  Collected: %s", f.path)
			sb.WriteString(fmt.Sprintf("**File: %s**\n```\n%s\n```\n\n", f.path, f.content))
			filesAnalyzed = append(filesAnalyzed, f.path)
		}
	}
	return sb.String(), filesAnalyzed, nil
}

func collectAllTextFiles(root, projectDir string, maxTotalSize int64, excludePatterns []string, progress *ui.Progress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".utopia" {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}
		if matchesAnyPattern(relPath, excludePatterns) {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "excluded by pattern"})
			}
			return nil
		}
		if totalSize+info.Size() > maxTotalSize {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !isTextFile(content) {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "binary file"})
			}
			return nil
		}
		files = append(files, collectedFile{path: relPath, content: truncateContent(string(content), 5000)})
		totalSize += info.Size()
		return nil
	})
	return files, skipped, err
}

func collectDomainContextIncremental(projectDir string, lastRun time.Time, incrementalMode bool, scope Scope, progress *ui.Progress) (string, map[string]time.Time, error) {
	var sb strings.Builder
	filesAnalyzed := make(map[string]time.Time)
	typeAnalyzer := types.NewAnalyzer()
	var allDiscoveredTypes []*types.DiscoveredType

	patterns := []struct {
		name    string
		glob    string
		maxSize int64
	}{
		{"Go Type Definitions", "**/*.go", 50000},
		{"YAML Schemas/Config", "**/*.yaml", 15000},
		{"JSON Schemas", "**/*.json", 15000},
		{"Protocol Buffers", "**/*.proto", 20000},
		{"GraphQL Schemas", "**/*.graphql", 15000},
		{"TypeScript Types", "**/*.ts", 30000},
	}

	searchRoots := scope.Paths
	if len(searchRoots) == 0 {
		searchRoots = []string{projectDir}
	} else {
		absoluteRoots := make([]string, 0, len(searchRoots))
		for _, p := range searchRoots {
			if filepath.IsAbs(p) {
				absoluteRoots = append(absoluteRoots, p)
			} else {
				absoluteRoots = append(absoluteRoots, filepath.Join(projectDir, p))
			}
		}
		searchRoots = absoluteRoots
	}

	for _, p := range patterns {
		var allFiles []collectedFile
		for _, root := range searchRoots {
			files, skipped, err := collectDomainFilesIncremental(root, projectDir, p.glob, p.maxSize, lastRun, incrementalMode, scope.ExcludePatterns, progress)
			if err != nil {
				continue
			}
			allFiles = append(allFiles, files...)
			for _, skip := range skipped {
				progress.Verbosef("\n  Skipped: %s (%s)", skip.path, skip.reason)
			}
		}
		if len(allFiles) > 0 {
			sb.WriteString(fmt.Sprintf("\n### %s\n\n", p.name))
			for _, f := range allFiles {
				progress.Verbosef("\n  Collected: %s", f.path)
				sb.WriteString(fmt.Sprintf("**File: %s**\n```\n%s\n```\n\n", f.path, f.content))
				filesAnalyzed[f.path] = f.modTime
				if strings.HasSuffix(f.path, ".go") {
					discoveredTypes := typeAnalyzer.AnalyzeGoFile(f.path, f.content)
					allDiscoveredTypes = append(allDiscoveredTypes, discoveredTypes...)
				} else if strings.HasSuffix(f.path, ".ts") {
					discoveredTypes := typeAnalyzer.AnalyzeTypeScriptFile(f.path, f.content)
					allDiscoveredTypes = append(allDiscoveredTypes, discoveredTypes...)
				}
			}
		}
	}

	if len(allDiscoveredTypes) > 0 {
		bcAnalyzer := types.NewBoundedContextAnalyzer()
		contextTerms := bcAnalyzer.GroupTermsByContext(allDiscoveredTypes)
		crossContextTerms := bcAnalyzer.FindCrossContextTerms(contextTerms)

		sb.WriteString("\n### Pre-Analyzed Domain Terms by Bounded Context\n\n")
		sb.WriteString("Terms are grouped by their inferred bounded context based on package structure.\n\n")

		var contextNames []string
		for ctx := range contextTerms {
			contextNames = append(contextNames, ctx)
		}
		sort.Strings(contextNames)

		for _, ctx := range contextNames {
			terms := contextTerms[ctx]
			if len(terms) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("#### Bounded Context: %s\n\n", ctx))
			for _, conf := range []types.TermConfidence{types.TermConfidenceHigh, types.TermConfidenceMedium, types.TermConfidenceLow} {
				var termsAtLevel []*types.ContextualTerm
				for _, term := range terms {
					if term.Confidence == conf {
						termsAtLevel = append(termsAtLevel, term)
					}
				}
				if len(termsAtLevel) > 0 {
					sb.WriteString(fmt.Sprintf("**%s Confidence:**\n", titleCase(string(conf))))
					for _, term := range termsAtLevel {
						typeKind := ""
						if len(term.Types) > 0 {
							typeKind = term.Types[0].Kind
						}
						sb.WriteString(fmt.Sprintf("- **%s**", term.Term))
						if typeKind != "" {
							sb.WriteString(fmt.Sprintf(" (%s)", typeKind))
						}
						sb.WriteString(fmt.Sprintf(" - found in %d file(s)\n", len(term.Files)))
						if otherContexts, exists := crossContextTerms[term.Term]; exists {
							var others []string
							for _, other := range otherContexts {
								if other != ctx {
									others = append(others, other)
								}
							}
							if len(others) > 0 {
								sb.WriteString(fmt.Sprintf("  - ⚠ Also appears in: %s\n", strings.Join(others, ", ")))
							}
						}
						evidenceLimit := 3
						for i, line := range term.Lines {
							if i >= evidenceLimit {
								sb.WriteString(fmt.Sprintf("  - ... and %d more locations\n", len(term.Lines)-evidenceLimit))
								break
							}
							sb.WriteString(fmt.Sprintf("  - %s\n", line))
						}
					}
					sb.WriteString("\n")
				}
			}
		}

		if len(crossContextTerms) > 0 {
			sb.WriteString("### Cross-Context Terms\n\n")
			for term, contexts := range crossContextTerms {
				sb.WriteString(fmt.Sprintf("- **%s**: appears in %s\n", term, strings.Join(contexts, ", ")))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String(), filesAnalyzed, nil
}

func collectDomainFilesIncremental(root, projectDir, pattern string, maxTotalSize int64, lastRun time.Time, incrementalMode bool, excludePatterns []string, progress *ui.Progress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".utopia" {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}
		if strings.HasSuffix(relPath, "_test.go") {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "test file"})
			}
			return nil
		}
		if strings.Contains(filepath.Base(relPath), "mock") {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "mock file"})
			}
			return nil
		}
		if strings.Contains(relPath, "generated") || strings.HasSuffix(relPath, ".gen.go") {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "generated file"})
			}
			return nil
		}
		if matchesAnyPattern(relPath, excludePatterns) {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "excluded by pattern"})
			}
			return nil
		}
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil || !matched {
			if !matchGlob(relPath, pattern) {
				return nil
			}
		}
		if incrementalMode && !info.ModTime().After(lastRun) {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "not modified since last run"})
			}
			return nil
		}
		if totalSize+info.Size() > maxTotalSize {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !isTextFile(content) {
			if progress.Verbose() {
				skipped = append(skipped, skippedFile{path: relPath, reason: "binary file"})
			}
			return nil
		}
		files = append(files, collectedFile{path: relPath, content: truncateContent(string(content), 5000), modTime: info.ModTime()})
		totalSize += info.Size()
		return nil
	})
	return files, skipped, err
}

func matchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(path, pattern) {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
	}
	return false
}

func matchGlob(path, pattern string) bool {
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if strings.HasSuffix(suffix, "/**") {
			dirPart := strings.TrimSuffix(suffix, "/**")
			return strings.HasPrefix(path, dirPart+"/") || path == dirPart
		}
		matched, _ := filepath.Match(suffix, filepath.Base(path))
		if matched {
			return true
		}
		if strings.Contains(suffix, "/") {
			return strings.Contains(path, strings.TrimPrefix(suffix, "**/"))
		}
	}
	return false
}

func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}
	for _, b := range content[:checkLen] {
		if b == 0 {
			return false
		}
	}
	return true
}

func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... [truncated]"
}
