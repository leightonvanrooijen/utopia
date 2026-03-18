package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
}

func runValidatorCreate(cmd *cobra.Command, args []string) error {
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
	creator := validators.NewCreator(projectDir)
	return creator.Run(ctx)
}

// ============================================================================
// EDIT - Edit an existing validator
// ============================================================================

var validatorEditCmd = &cobra.Command{
	Use:   "edit [validator-id]",
	Short: "Edit an existing validator",
	Long: `Edit an existing validator with AI assistance.

Opens a conversation to modify the validator's prompt, run trigger,
or allowed tools.

Examples:
  utopia validator edit component-standards
  utopia validator edit api-contracts`,
	Args: cobra.ExactArgs(1),
	RunE: runValidatorEdit,
}

func init() {
	validatorCmd.AddCommand(validatorEditCmd)
}

func runValidatorEdit(cmd *cobra.Command, args []string) error {
	// TODO: Implement validator editing with AI assistance
	return nil
}

// ============================================================================
// DELETE - Delete a validator
// ============================================================================

var validatorDeleteCmd = &cobra.Command{
	Use:   "delete [validator-id]",
	Short: "Delete a validator",
	Long: `Delete a validator from the project.

This removes the validator file from .utopia/validators/ and
updates the config to remove references to it.

Examples:
  utopia validator delete old-validator`,
	Args: cobra.ExactArgs(1),
	RunE: runValidatorDelete,
}

func init() {
	validatorCmd.AddCommand(validatorDeleteCmd)
}

func runValidatorDelete(cmd *cobra.Command, args []string) error {
	// TODO: Implement validator deletion
	return nil
}
