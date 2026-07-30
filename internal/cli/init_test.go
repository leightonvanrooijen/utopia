package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

// initProject runs init against a scratch directory, feeding the prompts from a
// pipe so the command can be exercised end to end rather than through its parts.
func initProject(t *testing.T, projectDir string) {
	t.Helper()
	initProjectOutput(t, projectDir)
}

// initProjectOutput runs init and returns what it printed to stdout.
func initProjectOutput(t *testing.T, projectDir string) string {
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

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("project", projectDir, "")
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	if err := runInit(cmd, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	return out.String()
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

// Writing the validator file is not enough: nothing runs an unregistered
// validator, so a fresh project needs the entry in config to reach escalation
// routing at all.
func TestInitRegistersSpecIntentValidator(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config after init: %v", err)
	}

	entry := findValidator(t, config, "validators/spec-intent.md")
	if !entry.GetAlways() {
		t.Error("spec-intent is not always: true; the relevance router could skip the validator routing depends on")
	}
	if config.Models == nil || config.Models.Validators == "" {
		t.Fatal("init left models.validators unset")
	}
	if got, want := config.Models.Validators, domain.DefaultValidatorModel; got != want {
		t.Errorf("models.validators = %q, want %q", got, want)
	}
	if err := domain.ValidateModelConfig(config.Models); err != nil {
		t.Errorf("written models config does not load: %v", err)
	}
}

// The registration is a config field init filled in, so it belongs in the same
// added/skipped report as verification.command.
func TestInitReportsSpecIntentRegistration(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	// A re-init is what prints the added/skipped report; by then both fields are
	// already configured, so they must show up as skipped rather than silently.
	out := initProjectOutput(t, projectDir)
	for _, field := range []string{"validators[validators/spec-intent.md]", "models.validators"} {
		if !strings.Contains(out, field) {
			t.Errorf("re-init report never mentions %q:\n%s", field, out)
		}
	}
}

// Re-init is additive: an entry the project already has (possibly with its own
// always value) is left exactly as it stands.
func TestInitDoesNotDuplicateExistingSpecIntentEntry(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config after init: %v", err)
	}
	// A project that deliberately routed the validator rather than always-running it.
	for i := range config.Validators {
		if config.Validators[i].Path == "validators/spec-intent.md" {
			config.Validators[i].Always = false
		}
	}
	config.Models.Validators = "sonnet"
	if err := store.SaveConfig(config); err != nil {
		t.Fatalf("failed to save edited config: %v", err)
	}

	initProject(t, projectDir)

	after, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config after re-init: %v", err)
	}
	var count int
	for _, v := range after.Validators {
		if v.GetPath() == "validators/spec-intent.md" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 spec-intent entry after re-init, got %d", count)
	}
	if entry := findValidator(t, after, "validators/spec-intent.md"); entry.GetAlways() {
		t.Error("re-init overwrote the project's always: false choice")
	}
	if after.Models.Validators != "sonnet" {
		t.Errorf("re-init overwrote models.validators: got %q, want %q", after.Models.Validators, "sonnet")
	}
}

// Registration matches on path, not list length, so unrelated validators a
// project already registered survive and spec-intent is still added.
func TestInitAddsSpecIntentAlongsideExistingValidators(t *testing.T) {
	projectDir := t.TempDir()
	initProject(t, projectDir)

	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config after init: %v", err)
	}
	config.Validators = []domain.ValidatorConfig{
		{Path: "validators/security.md", Model: "opus", Run: "after-phase"},
	}
	if err := store.SaveConfig(config); err != nil {
		t.Fatalf("failed to save edited config: %v", err)
	}

	initProject(t, projectDir)

	after, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config after re-init: %v", err)
	}
	if len(after.Validators) != 2 {
		t.Fatalf("expected 2 validators after re-init, got %d: %+v", len(after.Validators), after.Validators)
	}
	security := findValidator(t, after, "validators/security.md")
	if security.GetModel() != "opus" || security.GetRun() != "after-phase" {
		t.Errorf("re-init changed the existing entry: %+v", security)
	}
	if entry := findValidator(t, after, "validators/spec-intent.md"); !entry.GetAlways() {
		t.Error("spec-intent was not registered alongside the existing validator")
	}
}

// findValidator returns the config entry for path, failing the test if absent.
func findValidator(t *testing.T, config *domain.Config, path string) domain.ValidatorConfig {
	t.Helper()
	for _, v := range config.Validators {
		if v.GetPath() == path {
			return v
		}
	}
	t.Fatalf("config has no validators entry for %s: %+v", path, config.Validators)
	return domain.ValidatorConfig{}
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
