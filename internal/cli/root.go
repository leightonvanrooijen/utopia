package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/spf13/cobra"
)

// Version information - injected at build time via ldflags
var (
	Version   = "dev"     // -X github.com/leightonvanrooijen/utopia/internal/cli.Version=v1.0.0
	Commit    = "none"    // -X github.com/leightonvanrooijen/utopia/internal/cli.Commit=$(git rev-parse --short HEAD)
	BuildDate = "unknown" // -X github.com/leightonvanrooijen/utopia/internal/cli.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
)

var rootCmd = &cobra.Command{
	Use:   "utopia",
	Short: "AI-assisted development system",
	Long: `Utopia is a two-layer system where humans define intent through conversation,
and AI agents execute the work using the Ralph methodology.

Core Workflow:

  init     Set up a new Utopia project
              ↓
  cr       Create a change request through guided conversation
              ↓
  execute  AI executes the CR using Ralph loops until complete
              ↓
  harvest  Extract ADRs, concepts, and domain knowledge from conversations

Quick Start:
  utopia init              # Initialize project
  utopia cr                # Define what you want to build
  utopia execute           # Let AI implement it
  utopia harvest           # Capture learnings

Other Commands:
  merge    Manually merge a CR (execute does this automatically)
  status   View project state
  report   Compare what past runs spent and achieved
  format   Format YAML files`,
}

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringP("project", "p", ".", "project directory")

	// Version flag
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(`Utopia {{.Version}}
Commit:  ` + Commit + `
Built:   ` + BuildDate + `
`)
}

// GetProjectDir returns the project directory from flags
func GetProjectDir(cmd *cobra.Command) string {
	dir, _ := cmd.Flags().GetString("project")
	return dir
}

// ResolveProjectDir resolves the project directory to an absolute path.
// Unlike ResolveProject, this does NOT check for .utopia existence,
// making it suitable for 'init' which creates the .utopia directory.
func ResolveProjectDir(cmd *cobra.Command) (string, error) {
	projectDir := GetProjectDir(cmd)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project path: %w", err)
	}

	return absPath, nil
}

// ResolveProject resolves the project path, checks for .utopia directory,
// and returns the initialized store. This handles the common pattern used
// by most CLI commands that require an initialized Utopia project.
func ResolveProject(cmd *cobra.Command) (projectDir, utopiaDir string, store *internal.YAMLStore, err error) {
	projectDir = GetProjectDir(cmd)

	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir = filepath.Join(projectDir, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return "", "", nil, fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store = internal.NewYAMLStore(utopiaDir)

	return projectDir, utopiaDir, store, nil
}

// ResolveModelFlag validates the --model flag value and returns it unchanged for
// the claude CLI to resolve. Returns the empty string when the flag was not
// provided, which leaves the CLI on its own default, or an error if the value is
// neither a recognised alias nor a plausible model identifier.
//
// The value is forwarded verbatim: the CLI resolves an alias to the current
// generation of that model, so expanding it here would pin the invocation to
// whichever model was current when Utopia was built.
func ResolveModelFlag(cmd *cobra.Command) (string, error) {
	model, _ := cmd.Flags().GetString("model")
	if model == "" {
		return "", nil
	}

	if err := domain.ValidateModel(model); err != nil {
		return "", err
	}

	return model, nil
}

// effortFlagUsage is the help text for --effort on the single-session commands.
// They resolve effort the same way they resolve a model - from the flag, with no
// configured value behind it - so the flag's absence means the claude CLI's own
// default rather than a level from config.
const effortFlagUsage = "reasoning effort per turn (low, medium, high, xhigh, max); omit to use the claude CLI default"

// ResolveEffortFlag validates the --effort flag value and returns it for the
// claude CLI to apply, mirroring ResolveModelFlag. Returns the empty string when
// the flag was not provided, which leaves the configured effort in place, or an
// error if the value is not a recognised level.
//
// The override is per invocation, like --model: it is not a way to raise a role's
// effort mid-run, because nothing in the loop does that.
func ResolveEffortFlag(cmd *cobra.Command) (string, error) {
	effort, _ := cmd.Flags().GetString("effort")
	if effort == "" {
		return "", nil
	}

	if err := domain.ValidateEffort(effort); err != nil {
		return "", err
	}

	return effort, nil
}

// ResolveAuth resolves the credential this invocation authenticates with - the
// --auth flag winning over config.auth.mode - reports it, and returns the mode
// for the spawn sites to run with.
//
// The line is printed here, once, rather than where claude is spawned. A single
// command runs claude many times: ralph loops until a work item completes and
// discover refines candidates in parallel goroutines, all sharing the one
// credential resolved for the invocation. Reporting belongs to the invocation
// that made the choice, not to each subprocess that inherits it.
//
// Reporting also happens before any work starts, so the account that pays is on
// screen before the first token is spent rather than after. In api-key mode that
// makes a run with no key anywhere fail here instead of at the first spawn.
//
// Nothing is printed when no mode is selected: there is no credential switch to
// announce, and existing projects see the output they always saw.
func ResolveAuth(cmd *cobra.Command) (domain.AuthMode, error) {
	override, err := ResolveAuthFlag(cmd)
	if err != nil {
		return "", err
	}

	utopiaDir, authConfig, err := projectAuthConfig(cmd)
	if err != nil {
		return "", err
	}

	mode := domain.ResolveAuthMode(override, authConfig)

	selection, err := internal.ResolveAuthSelection(mode, utopiaDir, os.Environ())
	if err != nil {
		return "", err
	}

	// A diagnostic, not a result: it must not land in the stdout a caller pipes.
	if report := selection.Report(); report != "" {
		ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr()).Progressf("%s\n", report)
	}

	return mode, nil
}

// projectAuthConfig returns the project's .utopia directory and its configured
// auth section.
//
// A project with no config.yaml has no auth section, which reads the same as an
// omitted one - the commands that need an initialized project report that
// themselves, and auth reporting is not the place to raise it. Any other read
// failure is surfaced: a config that cannot be parsed must not be quietly
// downgraded to "no credential selected", since that is the reading that bills
// the wrong account.
func projectAuthConfig(cmd *cobra.Command) (utopiaDir string, authConfig *domain.AuthConfig, err error) {
	projectDir, err := ResolveProjectDir(cmd)
	if err != nil {
		return "", nil, err
	}
	utopiaDir = filepath.Join(projectDir, ".utopia")

	config, err := internal.NewYAMLStore(utopiaDir).LoadConfig()
	if errors.Is(err, os.ErrNotExist) {
		return utopiaDir, nil, nil
	}
	if err != nil {
		return "", nil, err
	}

	return utopiaDir, config.Auth, nil
}

// ResolveAuthFlag validates and resolves the --auth flag value.
// Returns the requested auth mode if valid, the empty mode if not provided
// (meaning the mode from config.auth applies), or an error if the value is
// invalid. Pair with domain.ResolveAuthMode to apply the override.
func ResolveAuthFlag(cmd *cobra.Command) (domain.AuthMode, error) {
	mode, _ := cmd.Flags().GetString("auth")
	if mode == "" {
		return "", nil
	}

	if err := domain.ValidateAuthMode(mode); err != nil {
		return "", err
	}

	return domain.AuthMode(mode), nil
}
