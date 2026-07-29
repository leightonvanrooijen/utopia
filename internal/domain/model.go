package domain

import (
	"fmt"
	"regexp"
)

// ModelName is a model alias understood by the claude CLI - a short, memorable
// name the CLI itself resolves to the current generation of that model.
type ModelName string

const (
	// ModelHaiku is the fastest, most cost-effective model.
	ModelHaiku ModelName = "haiku"
	// ModelSonnet is the balanced model for most use cases.
	ModelSonnet ModelName = "sonnet"
	// ModelOpus is the most capable model for complex tasks.
	ModelOpus ModelName = "opus"
	// ModelFable is the highest-capability model, for the most demanding work.
	ModelFable ModelName = "fable"
)

// modelAliases is every alias Utopia recognises. Utopia stores no model
// identifiers of its own: the CLI resolves an alias to whichever model is
// current at invocation time, so translating one here would replace a
// self-updating alias with a pin that silently expires when that model retires.
var modelAliases = []ModelName{ModelHaiku, ModelSonnet, ModelOpus, ModelFable}

// modelIdentifierPattern matches the shape of a full Claude model identifier,
// including the optional bracketed context-window suffix the CLI accepts. It is
// a plausibility check, not an existence check - the CLI is the authority on
// which identifiers exist, and this only exists so a typo in config fails at
// load time rather than at the first Claude invocation.
var modelIdentifierPattern = regexp.MustCompile(`^claude-[a-z0-9][a-z0-9.-]*(\[[a-z0-9]+\])?$`)

// ValidateModel reports whether name is something Utopia can hand to the claude
// CLI as its --model value: one of the recognised aliases, or a full model
// identifier for a caller who wants a specific pinned model. Either way the
// string is forwarded to the CLI unchanged; Utopia never rewrites it.
func ValidateModel(name string) error {
	for _, alias := range modelAliases {
		if ModelName(name) == alias {
			return nil
		}
	}
	if modelIdentifierPattern.MatchString(name) {
		return nil
	}
	return &InvalidModelError{Name: name}
}

// ValidModelNames returns all recognised model aliases.
func ValidModelNames() []ModelName {
	return append([]ModelName(nil), modelAliases...)
}

// aliasList renders the recognised aliases for error messages and flag help.
func aliasList() string {
	names := make([]string, 0, len(modelAliases))
	for _, alias := range modelAliases {
		names = append(names, string(alias))
	}
	return joinWithComma(names)
}

// InvalidModelError indicates a value that is neither a recognised alias nor a
// plausible model identifier.
type InvalidModelError struct {
	Name string
}

func (e *InvalidModelError) Error() string {
	return fmt.Sprintf("invalid model %q: use an alias (%s) or a full model identifier beginning with \"claude-\"",
		e.Name, aliasList())
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
			if err := ValidateModel(name); err != nil {
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

// ValidateValidatorModels validates the per-validator model overrides in the
// config's validators list. It exists so a typo in a validator's model fails at
// load time like one in the models section, rather than at the first Claude
// invocation - which for a validator is only reached after the work it gates on
// has already run.
func ValidateValidatorModels(vs []ValidatorConfig) error {
	var invalid []string
	for _, v := range vs {
		model := v.GetModel()
		if model == "" {
			continue
		}
		if err := ValidateModel(model); err != nil {
			invalid = append(invalid, fmt.Sprintf("validators[%s].model: %q", v.GetPath(), model))
		}
	}

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
	return fmt.Sprintf("invalid model configuration: %s (use an alias (%s) or a full model identifier beginning with \"claude-\")",
		joinWithComma(e.InvalidFields), aliasList())
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
