package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
	"github.com/spf13/cobra"
)

// validatorCmd is the base command for managing validators.
// Running it without subcommands shows help with available subcommands.
var validatorCmd = &cobra.Command{
	Use:   "validator",
	Short: "Manage project validators",
	Long: `Manage project validators that enforce code quality and standards.

Validators are markdown files with YAML frontmatter that define automated
checks run during change request execution. They can run after each work item,
after each phase, or on-demand.

Available Commands:
  create    Create a new validator with AI assistance
  edit      Edit an existing validator
  delete    Delete a validator

Examples:
  utopia validator create         # Start guided validator creation
  utopia validator edit my-validator
  utopia validator delete old-validator`,
}

func init() {
	rootCmd.AddCommand(validatorCmd)
}

// ============================================================================
// CREATE - Create a new validator
// ============================================================================

var (
	validatorCreateModelFlag  string
	validatorCreateEffortFlag string
	validatorCreateAuthFlag   string
)

var validatorCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new validator with AI assistance",
	Long: `Create a new validator through guided conversation with AI.

The assistant will help you define:
  - What the validator should check
  - When it should run (after-workitem, after-phase, on-demand)
  - Which tools it needs access to

The validator will be saved to .utopia/validators/.

Examples:
  utopia validator create`,
	RunE: runValidatorCreate,
}

func init() {
	validatorCmd.AddCommand(validatorCreateCmd)
	validatorCreateCmd.Flags().StringVar(&validatorCreateModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	validatorCreateCmd.Flags().StringVar(&validatorCreateEffortFlag, "effort", "", effortFlagUsage)
	validatorCreateCmd.Flags().StringVar(&validatorCreateAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

func runValidatorCreate(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model and effort flags early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	effort, err := ResolveEffortFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
	if err != nil {
		return err
	}

	// Get the project directory (current working directory)
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create a context that cancels on interrupt signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Run the validator creation assistant
	creator := validators.NewCreator(projectDir).WithAuth(authMode).WithPrinter(out)
	if modelID != "" {
		creator = creator.WithModel(modelID)
	}
	if effort != "" {
		creator = creator.WithEffort(effort)
	}
	return creator.Run(ctx)
}

// ============================================================================
// EDIT - Edit an existing validator
// ============================================================================

var (
	validatorEditModelFlag  string
	validatorEditEffortFlag string
	validatorEditAuthFlag   string
)

var validatorEditCmd = &cobra.Command{
	Use:   "edit [validator-id]",
	Short: "Edit an existing validator",
	Long: `Edit an existing validator with AI assistance.

If no validator ID is provided, lists all configured validators and
prompts for selection. You can select by number or by validator ID.

Opens a conversation to modify the validator's prompt, run trigger,
or allowed tools.

Examples:
  utopia validator edit                    # List and select
  utopia validator edit component-standards
  utopia validator edit api-contracts`,
	Args: cobra.MaximumNArgs(1),
	RunE: runValidatorEdit,
}

func init() {
	validatorCmd.AddCommand(validatorEditCmd)
	validatorEditCmd.Flags().StringVar(&validatorEditModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	validatorEditCmd.Flags().StringVar(&validatorEditEffortFlag, "effort", "", effortFlagUsage)
	validatorEditCmd.Flags().StringVar(&validatorEditAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

func runValidatorEdit(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model and effort flags early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	effort, err := ResolveEffortFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
	if err != nil {
		return err
	}

	// Get the project directory (current working directory)
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create the editor
	editor := validators.NewEditor(projectDir).WithAuth(authMode).WithPrinter(out)
	if modelID != "" {
		editor = editor.WithModel(modelID)
	}
	if effort != "" {
		editor = editor.WithEffort(effort)
	}

	// List available validators
	validatorList, err := editor.ListValidators()
	if err != nil {
		return fmt.Errorf("failed to list validators: %w", err)
	}

	// Determine which validator to edit
	var selectedPath string
	if len(args) > 0 {
		// User provided a validator ID - find it in the list
		selectedPath, err = findValidatorByID(validatorList, args[0])
		if err != nil {
			return err
		}
	} else {
		// No argument provided - show list and prompt for selection
		selectedPath, err = promptValidatorSelection(out, validatorList)
		if err != nil {
			return err
		}
	}

	// Create a context that cancels on interrupt signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Run the validator edit assistant
	return editor.Run(ctx, selectedPath)
}

// findValidatorByID finds a validator by its ID in the list.
// Returns the path if found, or an error with helpful suggestions.
func findValidatorByID(validatorList []validators.ValidatorInfo, id string) (string, error) {
	for _, v := range validatorList {
		if v.Validator.ID == id {
			return v.Path, nil
		}
	}

	// Build helpful error message with available validators
	var ids []string
	for _, v := range validatorList {
		ids = append(ids, v.Validator.ID)
	}
	return "", fmt.Errorf("validator %q not found (available validators: %s)", id, strings.Join(ids, ", "))
}

// promptValidatorSelection displays the list of validators and prompts the user to select one.
func promptValidatorSelection(out *ui.Printer, validatorList []validators.ValidatorInfo) (string, error) {
	out.Printf("Available validators:\n\n")

	for i, v := range validatorList {
		runTrigger := v.Validator.GetRun()
		out.Printf("  %d. %s (%s)\n", i+1, v.Validator.ID, runTrigger)
	}

	out.Printf("\nSelect validator (number or ID): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("no validator selected")
	}

	// Try to parse as number first
	if num, err := strconv.Atoi(input); err == nil {
		if num < 1 || num > len(validatorList) {
			return "", fmt.Errorf("invalid selection: %d (must be 1-%d)", num, len(validatorList))
		}
		return validatorList[num-1].Path, nil
	}

	// Otherwise treat as validator ID
	return findValidatorByID(validatorList, input)
}

// ============================================================================
// DELETE - Delete a validator
// ============================================================================

var validatorDeleteCmd = &cobra.Command{
	Use:   "delete [validator-id]",
	Short: "Delete a validator",
	Long: `Delete a validator from the project.

If no validator ID is provided, lists all configured validators and
prompts for selection. You can select by number or by validator ID.

This removes the validator file from .utopia/validators/ and
updates the config to remove references to it.

Examples:
  utopia validator delete                    # List and select
  utopia validator delete old-validator`,
	Args: cobra.MaximumNArgs(1),
	RunE: runValidatorDelete,
}

func init() {
	validatorCmd.AddCommand(validatorDeleteCmd)
}

func runValidatorDelete(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Get the project directory (current working directory)
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create the editor to list validators
	editor := validators.NewEditor(projectDir).WithPrinter(out)

	// List available validators
	validatorList, err := editor.ListValidators()
	if err != nil {
		return fmt.Errorf("failed to list validators: %w", err)
	}

	// Determine which validator to delete
	var selectedPath string
	var selectedValidator *validators.ValidatorInfo
	if len(args) > 0 {
		// User provided a validator ID - find it in the list
		selectedPath, err = findValidatorByID(validatorList, args[0])
		if err != nil {
			return err
		}
		// Find the validator info for display
		for i := range validatorList {
			if validatorList[i].Path == selectedPath {
				selectedValidator = &validatorList[i]
				break
			}
		}
	} else {
		// No argument provided - show list and prompt for selection
		selectedPath, err = promptValidatorSelection(out, validatorList)
		if err != nil {
			return err
		}
		// Find the validator info for display
		for i := range validatorList {
			if validatorList[i].Path == selectedPath {
				selectedValidator = &validatorList[i]
				break
			}
		}
	}

	if selectedValidator == nil {
		return fmt.Errorf("validator not found")
	}

	// Display what will be deleted
	out.Printf("\nValidator to delete:\n")
	out.Printf("  ID:   %s\n", selectedValidator.Validator.ID)
	out.Printf("  Run:  %s\n", selectedValidator.Validator.GetRun())
	out.Printf("  File: .utopia/%s\n", selectedPath)

	// Prompt for confirmation
	out.Printf("\nAre you sure you want to delete this validator? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))
	if input != "y" && input != "yes" {
		out.Printf("Deletion cancelled.\n")
		return nil
	}

	// Delete the validator using the deleter
	deleter := validators.NewDeleter(projectDir)
	if err := deleter.Delete(selectedPath); err != nil {
		return fmt.Errorf("failed to delete validator %s: %w", selectedValidator.Validator.ID, err)
	}

	out.Printf("\n")
	out.Successf("Deleted validator: %s", selectedValidator.Validator.ID)
	out.Printf("  Removed file: .utopia/%s\n", selectedPath)
	out.Printf("  Updated config: .utopia/config.yaml\n")

	return nil
}
