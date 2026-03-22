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
