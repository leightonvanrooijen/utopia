package validators

import (
	_ "embed"
)

// validatorAssistantPrompt contains the shared knowledge base and best practices
// for the validator creation and editing assistants.
//
//go:embed validator_assistant_prompt.md
var validatorAssistantPrompt string
