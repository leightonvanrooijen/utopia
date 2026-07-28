package domain

import "fmt"

// AuthMode selects which Anthropic credential the claude subprocess
// authenticates with.
type AuthMode string

const (
	// AuthModeAPIKey authenticates with an ANTHROPIC_API_KEY, so usage bills
	// to the API account.
	AuthModeAPIKey AuthMode = "api-key"
	// AuthModeSubscription suppresses API-key credentials so the claude CLI
	// falls through to its OAuth profile, so usage bills to the subscription.
	AuthModeSubscription AuthMode = "subscription"
)

// ValidAuthModes returns all valid auth mode values.
func ValidAuthModes() []AuthMode {
	return []AuthMode{AuthModeAPIKey, AuthModeSubscription}
}

// AuthConfig selects the credential used for every claude subprocess the
// project spawns. Omit the section entirely to inherit the ambient
// environment, which is the pre-auth behaviour.
//
// Example:
//
//	auth:
//	  mode: subscription
type AuthConfig struct {
	// Mode is "api-key" or "subscription". An empty value means no selection
	// was made, and the subprocess environment is inherited unchanged.
	Mode string `yaml:"mode,omitempty"`
}

// GetMode returns the configured auth mode, or the empty string when no mode
// is selected. Safe to call on a nil config, which is the omitted-section case.
func (ac *AuthConfig) GetMode() AuthMode {
	if ac == nil {
		return ""
	}
	return AuthMode(ac.Mode)
}

// ValidateAuthMode validates a single auth mode value.
// An empty value is valid and means no selection was made.
func ValidateAuthMode(mode string) error {
	if mode == "" {
		return nil
	}
	for _, valid := range ValidAuthModes() {
		if AuthMode(mode) == valid {
			return nil
		}
	}
	return &InvalidAuthModeError{Mode: mode}
}

// ResolveAuthMode picks the auth mode a single invocation runs with.
// A mode supplied per-invocation (the --auth flag) wins over the configured
// mode; when none is supplied the configured mode applies; when neither is
// set the empty mode means "inherit the ambient environment".
func ResolveAuthMode(override AuthMode, ac *AuthConfig) AuthMode {
	if override != "" {
		return override
	}
	return ac.GetMode()
}

// ValidateAuthConfig validates the auth section of a config.
// Returns nil if the config is nil or the mode is valid.
func ValidateAuthConfig(ac *AuthConfig) error {
	if ac == nil {
		return nil
	}
	return ValidateAuthMode(ac.Mode)
}

// InvalidAuthModeError indicates an unrecognized auth mode was provided.
type InvalidAuthModeError struct {
	Mode string
}

func (e *InvalidAuthModeError) Error() string {
	return fmt.Sprintf("invalid auth mode %q: valid options are api-key, subscription", e.Mode)
}

// Is allows errors.Is to match any InvalidAuthModeError regardless of the mode.
func (e *InvalidAuthModeError) Is(target error) bool {
	_, ok := target.(*InvalidAuthModeError)
	return ok
}
