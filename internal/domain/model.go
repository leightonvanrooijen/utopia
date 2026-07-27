package domain

import "fmt"

// ModelName represents a user-friendly model identifier.
// These are short, memorable names that map to full Claude model IDs.
type ModelName string

const (
	// ModelHaiku is the fastest, most cost-effective model.
	ModelHaiku ModelName = "haiku"
	// ModelSonnet is the balanced model for most use cases.
	ModelSonnet ModelName = "sonnet"
	// ModelOpus is the most capable model for complex tasks.
	ModelOpus ModelName = "opus"
)

// modelMappings maps user-friendly names to Claude model identifiers.
var modelMappings = map[ModelName]string{
	ModelHaiku:  "claude-3-5-haiku-20241022",
	ModelSonnet: "claude-sonnet-4-20250514",
	ModelOpus:   "claude-opus-4-20250514",
}

// ResolveModel converts a user-friendly model name to the full Claude model identifier.
// Returns an error if the model name is not recognized.
func ResolveModel(name string) (string, error) {
	modelID, ok := modelMappings[ModelName(name)]
	if !ok {
		return "", &InvalidModelError{Name: name}
	}
	return modelID, nil
}

// ValidModelNames returns all valid user-friendly model names.
func ValidModelNames() []ModelName {
	return []ModelName{ModelHaiku, ModelSonnet, ModelOpus}
}

// InvalidModelError indicates an unrecognized model name was provided.
type InvalidModelError struct {
	Name string
}

func (e *InvalidModelError) Error() string {
	return fmt.Sprintf("invalid model name %q: valid options are haiku, sonnet, opus", e.Name)
}

// Is allows errors.Is to match any InvalidModelError regardless of the name.
func (e *InvalidModelError) Is(target error) bool {
	_, ok := target.(*InvalidModelError)
	return ok
}

// ValidateModelConfig validates all model names in a ModelConfig.
// Returns nil if the config is nil or all model names are valid.
// Returns an error describing all invalid model names found.
func ValidateModelConfig(mc *ModelConfig) error {
	if mc == nil {
		return nil
	}

	var invalid []string
	checkModel := func(name, field string) {
		if name != "" {
			if _, err := ResolveModel(name); err != nil {
				invalid = append(invalid, fmt.Sprintf("%s: %q", field, name))
			}
		}
	}

	checkModel(mc.Default, "models.default")
	checkModel(mc.CR, "models.cr")
	checkModel(mc.Harvest, "models.harvest")
	checkModel(mc.Execute, "models.execute")
	checkModel(mc.Validators, "models.validators")
	checkModel(mc.ValidatorRouter, "models.validator_router")
	checkModel(mc.Discover, "models.discover")
	checkModel(mc.Standards, "models.standards")
	checkModel(mc.Refactor, "models.refactor")
	checkModel(mc.Shape, "models.shape")
	checkModel(mc.ValidatorCreate, "models.validator_create")
	checkModel(mc.ValidatorEdit, "models.validator_edit")

	if len(invalid) == 0 {
		return nil
	}

	return &InvalidModelConfigError{InvalidFields: invalid}
}

// InvalidModelConfigError indicates one or more invalid model names in config.
type InvalidModelConfigError struct {
	InvalidFields []string
}

func (e *InvalidModelConfigError) Error() string {
	return fmt.Sprintf("invalid model configuration: %s (valid options: haiku, sonnet, opus)",
		joinWithComma(e.InvalidFields))
}

// Is allows errors.Is to match any InvalidModelConfigError.
func (e *InvalidModelConfigError) Is(target error) bool {
	_, ok := target.(*InvalidModelConfigError)
	return ok
}

// joinWithComma joins strings with ", " separator.
func joinWithComma(s []string) string {
	if len(s) == 0 {
		return ""
	}
	if len(s) == 1 {
		return s[0]
	}
	result := s[0]
	for i := 1; i < len(s); i++ {
		result += ", " + s[i]
	}
	return result
}
