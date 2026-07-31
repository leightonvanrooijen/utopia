package validators

import (
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/layout"
)

// Deleter handles the deletion of validators.
type Deleter struct {
	projectDir string
	store      *internal.YAMLStore
}

// NewDeleter creates a new validator deleter for the given project directory.
func NewDeleter(projectDir string) *Deleter {
	utopiaDir := layout.Root(projectDir)
	return &Deleter{
		projectDir: projectDir,
		store:      internal.NewYAMLStore(utopiaDir),
	}
}

// Delete removes a validator file and updates the config to remove the reference.
// The validatorPath should be relative to .utopia/ (e.g., "validators/my-validator.md").
func (d *Deleter) Delete(validatorPath string) error {
	// Load the current config
	config, err := d.store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Remove the validator from the config
	var updatedValidators []domain.ValidatorConfig
	found := false
	for _, vc := range config.Validators {
		if vc.GetPath() == validatorPath {
			found = true
			continue
		}
		updatedValidators = append(updatedValidators, vc)
	}

	if !found {
		return fmt.Errorf("validator %q not found in config", validatorPath)
	}

	// Update the config
	config.Validators = updatedValidators
	if err := d.store.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	// Delete the validator file
	if err := d.store.DeleteValidator(validatorPath); err != nil {
		return fmt.Errorf("failed to delete validator file: %w", err)
	}

	return nil
}
