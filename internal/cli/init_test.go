package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/spf13/cobra"
)

// initProject runs init against a scratch directory, feeding the prompts from a
// pipe so the command can be exercised end to end rather than through its parts.
func initProject(t *testing.T, projectDir string) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := writer.WriteString("make test\n3\nA test project.\n"); err != nil {
		t.Fatalf("failed to write init answers: %v", err)
	}
	writer.Close()

	realStdin := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = realStdin
		reader.Close()
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("project", projectDir, "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := runInit(cmd, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

// A fresh project has to start with the spec-intent validator on disk: a
// failure_class can only come from a validator verdict, so a project with no
// validators can never reach escalation routing.
func TestInitWritesSpecIntentValidator(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	path := filepath.Join(projectDir, ".utopia", "validators", "spec-intent.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("init did not create %s: %v", path, err)
	}

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	validator, err := store.LoadValidator(filepath.Join("validators", "spec-intent.md"))
	if err != nil {
		t.Fatalf("written validator does not parse: %v", err)
	}

	if validator.ID != "spec-intent" {
		t.Errorf("expected id %q, got %q", "spec-intent", validator.ID)
	}
	if validator.Description == "" {
		t.Error("validator description is empty; the run log and the router both read it")
	}
	if got := strings.Join(validator.AllowedTools, ","); got != "Read,Glob,Grep" {
		t.Errorf("expected read-only tools [Read Glob Grep], got [%s]", got)
	}
}

// The runner parses one verdict block with a fixed set of field names. A prompt
// that invents or renames a field produces a verdict Utopia cannot route on.
func TestSpecIntentValidatorPromptUsesRunnerVerdictFields(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	validator, err := store.LoadValidator(filepath.Join("validators", "spec-intent.md"))
	if err != nil {
		t.Fatalf("written validator does not parse: %v", err)
	}
	prompt := validator.Prompt

	for _, field := range []string{
		"verdict", "failure_class", "diagnosis",
		"corrected_intent", "confidence", "spec_defect_suspected",
	} {
		if !strings.Contains(prompt, field) {
			t.Errorf("prompt never mentions verdict field %q", field)
		}
	}
	if !strings.Contains(prompt, "<VERDICT>") {
		t.Error("prompt does not ask for a <VERDICT> block")
	}
	if !strings.Contains(prompt, "{{changed_files}}") {
		t.Error("prompt has no {{changed_files}} placeholder, so it would review nothing")
	}
}

// The shipped validator goes into every repo, so it must not assume Utopia's own
// language, tooling or standards filenames.
func TestSpecIntentValidatorPromptIsRepoAgnostic(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	body, err := os.ReadFile(filepath.Join(projectDir, ".utopia", "validators", "spec-intent.md"))
	if err != nil {
		t.Fatalf("failed to read written validator: %v", err)
	}
	prompt := string(body)

	for _, banned := range []string{
		"./scripts/verify.sh", "go test", "npm test", "cargo", "pytest",
		"gofmt", "golangci", "cli-organization.md", "error-handling.md",
		"terminal-output.md",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("prompt hardcodes repo-specific %q", banned)
		}
	}

	if !strings.Contains(prompt, "verification.command") {
		t.Error("prompt does not point at the project's configured verification.command")
	}
	for _, key := range []string{"paths.specs", "paths.adrs"} {
		if !strings.Contains(prompt, key) {
			t.Errorf("prompt does not resolve artifact locations through %s", key)
		}
	}
	for _, fallback := range []string{".utopia/specs/", ".utopia/adrs/"} {
		if !strings.Contains(prompt, fallback) {
			t.Errorf("prompt does not name the %s default for an unset path", fallback)
		}
	}
	if !strings.Contains(prompt, "no written standards") {
		t.Error("prompt does not tell the validator to flag nothing when the project documents no standards")
	}
}

// A project that edits the shipped validator must keep those edits across a
// re-init, the same guarantee _template.md already has.
func TestInitPreservesExistingSpecIntentValidator(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	path := filepath.Join(projectDir, ".utopia", "validators", "spec-intent.md")
	customized := "---\nid: spec-intent\n---\n\nMy own prompt.\n"
	if err := os.WriteFile(path, []byte(customized), 0644); err != nil {
		t.Fatalf("failed to customize validator: %v", err)
	}

	initProject(t, projectDir)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read validator after re-init: %v", err)
	}
	if string(after) != customized {
		t.Errorf("re-init overwrote the existing validator:\ngot:\n%s\nwant:\n%s", after, customized)
	}
}
