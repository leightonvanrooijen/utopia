// Package layout owns the on-disk layout of a project's .utopia directory.
//
// It is the single answer to "where do work items live". Every other package
// asks it rather than rebuilding the layout from string literals, so moving or
// renaming a subdirectory is a change to this file alone.
//
// Two kinds of caller are served, because the codebase has two kinds of root in
// hand:
//
//   - Callers holding a project directory use the accessors - Root, Specs,
//     WorkItems and friends. These take the project directory explicitly; there
//     is no package-level "current project".
//   - Callers already rooted at the .utopia directory - the store, whose
//     baseDir is that directory, and the merge helpers it hands paths to -
//     compose with the exported subdirectory names instead. They cannot use the
//     accessors: a store base directory is not always literally named .utopia
//     (tests root one at a bare temp dir), so deriving a project directory back
//     out of it would resolve somewhere else entirely.
package layout

import "path/filepath"

// DirName is the name of the per-project directory holding every Utopia
// artifact. Traversals that skip the directory match on this name rather than
// on a path, so they share the constant without going through an accessor.
const DirName = ".utopia"

// Subdirectory names under DirName. Callers already rooted at the .utopia
// directory join these; callers holding a project directory use the accessors
// below, which are defined in terms of them.
const (
	SpecsName          = "specs"
	ADRsName           = "adrs"
	ConceptsName       = "concepts"
	DomainName         = "domain"
	DraftsName         = "drafts"
	NotesName          = "notes"
	ConversationsName  = "conversations"
	ChangeRequestsName = "change-requests"
	WorkItemsName      = "work-items"
	RunsName           = "runs"
	ValidatorsName     = "validators"
	StandardsName      = "standards"
)

// Root returns a project's .utopia directory. This is the directory api-key
// auth looks in for the .env holding the key, so it is also what callers hand
// to CLI.WithAuth.
func Root(projectDir string) string {
	return filepath.Join(projectDir, DirName)
}

// Specs returns the directory holding living specifications. This is the
// default location only: a project may relocate it via the paths section of
// config.yaml, which the store resolves.
func Specs(projectDir string) string {
	return filepath.Join(Root(projectDir), SpecsName)
}

// ADRs returns the default directory holding architecture decision records.
func ADRs(projectDir string) string {
	return filepath.Join(Root(projectDir), ADRsName)
}

// Concepts returns the default directory holding concept documents.
func Concepts(projectDir string) string {
	return filepath.Join(Root(projectDir), ConceptsName)
}

// Domain returns the default directory holding domain documents.
func Domain(projectDir string) string {
	return filepath.Join(Root(projectDir), DomainName)
}

// Drafts returns the directory holding drafts awaiting promotion, one
// subdirectory per artifact kind. See DraftSpecs and DraftDomain.
func Drafts(projectDir string) string {
	return filepath.Join(Root(projectDir), DraftsName)
}

// DraftSpecs returns the directory holding draft specifications, which require
// validation before promotion into Specs.
func DraftSpecs(projectDir string) string {
	return filepath.Join(Drafts(projectDir), SpecsName)
}

// DraftDomain returns the directory holding draft domain documents, which
// require validation before promotion into Domain.
func DraftDomain(projectDir string) string {
	return filepath.Join(Drafts(projectDir), DomainName)
}

// Notes returns the directory holding free-form notes captured during
// conversations - ideas that are not part of any change request.
func Notes(projectDir string) string {
	return filepath.Join(Root(projectDir), NotesName)
}

// Conversations returns the directory holding harvest sources. Unlike the
// artifact directories it is not configurable: conversations are written by the
// tool itself.
func Conversations(projectDir string) string {
	return filepath.Join(Root(projectDir), ConversationsName)
}

// ChangeRequests returns the directory holding change requests - the queue the
// execution loop re-scans.
func ChangeRequests(projectDir string) string {
	return filepath.Join(Root(projectDir), ChangeRequestsName)
}

// WorkItems returns the directory holding work items, one subdirectory per
// change request.
func WorkItems(projectDir string) string {
	return filepath.Join(Root(projectDir), WorkItemsName)
}

// Runs returns the directory holding execution run records, one subdirectory
// per change request.
func Runs(projectDir string) string {
	return filepath.Join(Root(projectDir), RunsName)
}

// Validators returns the directory holding validator documents.
func Validators(projectDir string) string {
	return filepath.Join(Root(projectDir), ValidatorsName)
}

// Standards returns the directory holding coding standards documents.
func Standards(projectDir string) string {
	return filepath.Join(Root(projectDir), StandardsName)
}

// StandardRelPath returns a standards document's path relative to the project
// root, slash-separated regardless of platform. The standards index carries
// these paths into stored metadata and into prompts, so they must stay stable
// across platforms rather than pick up the host separator.
func StandardRelPath(fileName string) string {
	return filepath.ToSlash(filepath.Join(DirName, StandardsName, fileName))
}
