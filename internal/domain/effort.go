package domain

import "fmt"

// EffortLevel is a reasoning-effort level the claude CLI understands. It is
// passed to the CLI's --effort flag unchanged: Utopia stores the level, the CLI
// decides what it costs.
type EffortLevel string

const (
	// EffortLow is the shallowest level: least reasoning per turn, cheapest.
	EffortLow EffortLevel = "low"
	// EffortMedium is the level a role runs at when nothing warrants more.
	EffortMedium EffortLevel = "medium"
	// EffortHigh is the level a role runs at when the question is harder than the
	// one that failed.
	EffortHigh EffortLevel = "high"
	// EffortXHigh is what Claude Code itself defaults to for coding and agentic
	// work - the obvious candidate if a role at high proves too shallow.
	EffortXHigh EffortLevel = "xhigh"
	// EffortMax is the deepest level, and the most expensive per turn.
	EffortMax EffortLevel = "max"
)

// effortLevels is every level Utopia recognises, in increasing depth. The order
// is documentation, not a ranking Utopia acts on: nothing in the loop compares
// two levels, because no code path raises a role's effort (see EffortConfig).
var effortLevels = []EffortLevel{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// Built-in effort per role, applied when neither the role's key nor
// effort.default is set. Each is a property of the role rather than a dial the
// loop turns: the default executor runs at medium because a failure escalates
// the model instead of cranking effort, and the escalated executor and scoper
// run at high because they are asked harder questions than the one that failed.
const (
	// DefaultExecutorEffort is the level a first-attempt execution runs at.
	DefaultExecutorEffort = EffortMedium
	// DefaultEscalatedExecutorEffort is the level an escalated execution attempt
	// runs at.
	DefaultEscalatedExecutorEffort = EffortHigh
	// DefaultValidatorEffort is the level a validator runs at.
	DefaultValidatorEffort = EffortMedium
	// DefaultScoperEffort is the level a change-request rewrite runs at.
	DefaultScoperEffort = EffortHigh
)

// ValidateEffort reports whether level is something Utopia can hand to the
// claude CLI as its --effort value. The set is closed, unlike models: a level is
// a fixed vocabulary the CLI defines, not an alias that resolves to whatever is
// current, so an unrecognised one is always a typo.
func ValidateEffort(level string) error {
	for _, known := range effortLevels {
		if EffortLevel(level) == known {
			return nil
		}
	}
	return &InvalidEffortError{Level: level}
}

// ValidEffortLevels returns all recognised effort levels, shallowest first.
func ValidEffortLevels() []EffortLevel {
	return append([]EffortLevel(nil), effortLevels...)
}

// effortList renders the recognised levels for error messages and flag help.
func effortList() string {
	names := make([]string, 0, len(effortLevels))
	for _, level := range effortLevels {
		names = append(names, string(level))
	}
	return joinWithComma(names)
}

// InvalidEffortError indicates a value that is not a recognised effort level.
type InvalidEffortError struct {
	Level string
}

func (e *InvalidEffortError) Error() string {
	return fmt.Sprintf("invalid effort %q: use one of %s", e.Level, effortList())
}

// Is allows errors.Is to match any InvalidEffortError regardless of the level.
func (e *InvalidEffortError) Is(target error) bool {
	_, ok := target.(*InvalidEffortError)
	return ok
}

// EffortConfig specifies how much reasoning each role gets per turn. Its keys
// mirror the models section exactly - effort.execute is to models.execute what
// effort.default is to models.default - so a reader who understands one
// understands the other. Omit the section entirely to run every role on its
// built-in default.
//
// Example:
//
//	effort:
//	  default: medium
//	  execute: medium
//	  execute_escalated: high
//	  scoper: high
//	  validators: medium
//
// Effort is not the same knob as work_items.turn_budget, which becomes the
// CLI's --max-turns. Both bound the cost of one invocation from different
// sides: effort bounds how much reasoning a single turn may spend, turn budget
// bounds how many turns there may be. Raising one does not compensate for
// lowering the other.
//
// Effort is a fixed property of a role, never a response to failure. Effort and
// model are separate levers with different economics: at the top of its effort
// range a cheaper model can cost more than the expensive model would have, for
// comparable quality, so escalating the model is strictly cheaper than cranking
// effort. A validation failure, a mechanical retry and an escalation therefore
// change which model runs, never how hard the cheap one tries - the default
// executor's effort is the same on its first attempt as on its last. See
// ADR-004.
type EffortConfig struct {
	// Default effort used when a role doesn't have a specific override.
	// If not set, each role falls back to its own built-in default.
	Default string `yaml:"default,omitempty"`

	// Per-role effort overrides
	CR      string `yaml:"cr,omitempty"`
	Harvest string `yaml:"harvest,omitempty"`
	Execute string `yaml:"execute,omitempty"`
	// ExecuteEscalated is the effort an escalated execution attempt runs at. It
	// is higher than Execute by default because the escalated executor is a
	// different role, not the default executor trying harder.
	ExecuteEscalated string `yaml:"execute_escalated,omitempty"`
	// Scoper is the effort a change-request rewrite runs at. It defaults high
	// because the scoper is asked what the change request should have said,
	// which is a harder question than the one the executor failed to answer.
	Scoper          string `yaml:"scoper,omitempty"`
	Validators      string `yaml:"validators,omitempty"`
	ValidatorRouter string `yaml:"validator_router,omitempty"`
	Discover        string `yaml:"discover,omitempty"`
	Standards       string `yaml:"standards,omitempty"`
	Refactor        string `yaml:"refactor,omitempty"`
	Shape           string `yaml:"shape,omitempty"`
	ValidatorCreate string `yaml:"validator_create,omitempty"`
	ValidatorEdit   string `yaml:"validator_edit,omitempty"`
}

// EffortForCommand returns the effort level for the given command:
// effort.<command> > effort.default > the empty string, which means no --effort
// flag is passed and the claude CLI default applies.
//
// Safe to call on a nil config, which is the omitted-section case.
func (c *EffortConfig) EffortForCommand(command string) string {
	if c == nil {
		return ""
	}

	var override string
	switch command {
	case "cr":
		override = c.CR
	case "harvest":
		override = c.Harvest
	case "execute":
		override = c.Execute
	case "execute_escalated":
		override = c.ExecuteEscalated
	case "scoper":
		override = c.Scoper
	case "validators":
		override = c.Validators
	case "validator_router":
		override = c.ValidatorRouter
	case "discover":
		override = c.Discover
	case "standards":
		override = c.Standards
	case "refactor":
		override = c.Refactor
	case "shape":
		override = c.Shape
	case "validator_create":
		override = c.ValidatorCreate
	case "validator_edit":
		override = c.ValidatorEdit
	}

	if override != "" {
		return override
	}
	return c.Default
}

// effortOr resolves one role's effort, falling back to the role's built-in
// default when neither the role's key nor effort.default is set.
func (c *EffortConfig) effortOr(command string, fallback EffortLevel) string {
	if resolved := c.EffortForCommand(command); resolved != "" {
		return resolved
	}
	return string(fallback)
}

// ExecutorEffort resolves the effort a first-attempt execution runs at:
// effort.execute > effort.default > medium.
//
// It is the same on every attempt on this executor, first or last. A failure
// escalates the model instead, which is the cheaper move.
func (c *EffortConfig) ExecutorEffort() string {
	return c.effortOr("execute", DefaultExecutorEffort)
}

// EscalatedExecutorEffort resolves the effort an escalated execution attempt
// runs at: effort.execute_escalated > effort.default > high.
func (c *EffortConfig) EscalatedExecutorEffort() string {
	return c.effortOr("execute_escalated", DefaultEscalatedExecutorEffort)
}

// ValidatorEffort resolves the effort a validator runs at:
// effort.validators > effort.default > medium.
func (c *EffortConfig) ValidatorEffort() string {
	return c.effortOr("validators", DefaultValidatorEffort)
}

// ScoperEffort resolves the effort a change-request rewrite runs at:
// effort.scoper > effort.default > high.
func (c *EffortConfig) ScoperEffort() string {
	return c.effortOr("scoper", DefaultScoperEffort)
}

// ValidateEffortConfig validates every effort level in an EffortConfig.
// Returns nil if the config is nil or all levels are recognised, and an error
// naming every offending key otherwise.
//
// It runs at config load time, alongside the model-name validation, so a
// mistyped level fails before a run starts rather than at the first claude
// invocation - which for the escalated executor is only reached after the cheap
// path has already been paid for.
func ValidateEffortConfig(ec *EffortConfig) error {
	if ec == nil {
		return nil
	}

	var invalid []string
	check := func(level, field string) {
		if level == "" {
			return
		}
		if err := ValidateEffort(level); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s: %q", field, level))
		}
	}

	check(ec.Default, "effort.default")
	check(ec.CR, "effort.cr")
	check(ec.Harvest, "effort.harvest")
	check(ec.Execute, "effort.execute")
	check(ec.ExecuteEscalated, "effort.execute_escalated")
	check(ec.Scoper, "effort.scoper")
	check(ec.Validators, "effort.validators")
	check(ec.ValidatorRouter, "effort.validator_router")
	check(ec.Discover, "effort.discover")
	check(ec.Standards, "effort.standards")
	check(ec.Refactor, "effort.refactor")
	check(ec.Shape, "effort.shape")
	check(ec.ValidatorCreate, "effort.validator_create")
	check(ec.ValidatorEdit, "effort.validator_edit")

	if len(invalid) == 0 {
		return nil
	}

	return &InvalidEffortConfigError{InvalidFields: invalid}
}

// InvalidEffortConfigError indicates one or more unrecognised effort levels in
// config.
type InvalidEffortConfigError struct {
	InvalidFields []string
}

func (e *InvalidEffortConfigError) Error() string {
	return fmt.Sprintf("invalid effort configuration: %s (use one of %s)",
		joinWithComma(e.InvalidFields), effortList())
}

// Is allows errors.Is to match any InvalidEffortConfigError.
func (e *InvalidEffortConfigError) Is(target error) bool {
	_, ok := target.(*InvalidEffortConfigError)
	return ok
}
