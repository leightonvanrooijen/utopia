package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Environment variables the claude CLI reads its Anthropic credential from.
const (
	// APIKeyEnvVar holds a long-lived Anthropic API key.
	APIKeyEnvVar = "ANTHROPIC_API_KEY"
	// AuthTokenEnvVar holds a bearer token. It shares the credential slot with
	// APIKeyEnvVar, and sending both makes the API reject the request with 401,
	// so whichever credential is selected the other must be absent.
	AuthTokenEnvVar = "ANTHROPIC_AUTH_TOKEN"
)

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

// CredentialSource names the location a credential was resolved from. The
// values are the user-facing location names, so they can be reported directly.
type CredentialSource string

const (
	// CredentialSourceEnvFile is the gitignored per-project credential file.
	CredentialSourceEnvFile CredentialSource = ".utopia/.env"
	// CredentialSourceEnvironment is the environment utopia itself inherited.
	CredentialSourceEnvironment CredentialSource = "the environment"
)

// APIKeyCredential is a resolved ANTHROPIC_API_KEY and the location it came
// from. The key is never printed; Source is what gets reported.
type APIKeyCredential struct {
	Key    string
	Source CredentialSource
}

// AuthSelection is the credential one invocation resolved, reduced to what can
// be said out loud: which mode won, where an api-key came from, and which
// ambient credential variables were removed. It deliberately holds no key, so
// no formatting mistake can leak one.
type AuthSelection struct {
	// Mode is the resolved auth mode, or empty when no selection was made.
	Mode AuthMode
	// Source is the location an api-key was resolved from. Empty in other modes.
	Source CredentialSource
	// Suppressed names the credential variables the mode dropped from the
	// inherited environment.
	Suppressed []string
}

// Report renders the one line a command prints before spawning claude, naming
// the account that pays so a switch of credential is never silent.
//
// The empty mode renders the empty string, and callers print nothing: no
// credential was selected, the subprocess inherits the environment utopia is
// running with, and there is no switch to announce. That is what keeps a project
// with no auth section looking exactly as it did before the feature existed.
//
// Each mode reports the fact that would surprise its user. api-key mode names
// the location, because a key in .utopia/.env silently outranking the shell's is
// the confusing case. subscription mode names what it ignored, because a key
// sitting in the environment while the subscription pays is the confusing case -
// a direnv .envrc exporting one is the motivating example.
func (s AuthSelection) Report() string {
	switch s.Mode {
	case "":
		return ""
	case AuthModeAPIKey:
		return fmt.Sprintf("Auth: %s (%s from %s)", AuthModeAPIKey, APIKeyEnvVar, s.Source)
	case AuthModeSubscription:
		if len(s.Suppressed) == 0 {
			return fmt.Sprintf("Auth: %s", AuthModeSubscription)
		}
		return fmt.Sprintf("Auth: %s (ambient %s ignored)",
			AuthModeSubscription, strings.Join(s.Suppressed, ", "))
	default:
		return fmt.Sprintf("Auth: %s", s.Mode)
	}
}

// SuppressedCredentialVars names the credential variables present in an
// environment that SubscriptionEnv removes from it, in a fixed order so the
// reported line is stable. A variable set to the empty string is not reported:
// there was no credential there to ignore.
func SuppressedCredentialVars(ambient []string) []string {
	var suppressed []string
	for _, name := range []string{APIKeyEnvVar, AuthTokenEnvVar} {
		if lookupEnv(ambient, name) != "" {
			suppressed = append(suppressed, name)
		}
	}
	return suppressed
}

// ResolveAPIKeyCredential picks the API key an api-key mode run authenticates
// with. A key in .utopia/.env wins over the ambient environment, so a project
// can pin the account that pays without unsetting whatever the shell exports.
// An empty value counts as absent in both locations.
//
// Returns a *MissingAPIKeyError when neither location holds a key. Callers must
// resolve before spawning claude, so an unauthenticated run fails immediately
// rather than after a subprocess starts.
func ResolveAPIKeyCredential(envFile map[string]string, ambient []string) (APIKeyCredential, error) {
	if key := envFile[APIKeyEnvVar]; key != "" {
		return APIKeyCredential{Key: key, Source: CredentialSourceEnvFile}, nil
	}
	if key := lookupEnv(ambient, APIKeyEnvVar); key != "" {
		return APIKeyCredential{Key: key, Source: CredentialSourceEnvironment}, nil
	}
	return APIKeyCredential{}, &MissingAPIKeyError{}
}

// Env returns the environment a claude subprocess should run with: every
// inherited variable unchanged, except that APIKeyEnvVar becomes the resolved
// key and AuthTokenEnvVar is dropped entirely.
//
// Both variables are filtered out of the inherited entries before the key is
// appended, so an ambient key is replaced rather than shadowed by a duplicate,
// and the token is removed outright rather than blanked - an empty value still
// occupies its slot in the credential precedence chain.
func (c APIKeyCredential) Env(ambient []string) []string {
	return append(withoutCredentialVars(ambient, 1), APIKeyEnvVar+"="+c.Key)
}

// ApplyEnvFile overlays the variables loaded from .utopia/.env onto an inherited
// environment, so a project can pin Anthropic settings in a gitignored file
// instead of relying on whatever the shell exports.
//
// The file wins over the ambient environment: a name defined in both places
// takes the file's value, written into the position the ambient entry held rather
// than appended as a second entry, so the ambient value is replaced outright.
// Names only the file defines are appended in sorted order, which keeps the
// result deterministic despite map iteration being randomised. When the file
// contributes nothing, ambient is returned unchanged.
//
// The two credential variables are skipped here because the auth mode owns that
// slot. api-key mode has already resolved APIKeyEnvVar with the file taking
// precedence, and dropped AuthTokenEnvVar; reapplying the file's copy of either
// would put both credentials in the environment at once, which the API answers
// with a 401. Overlaying only the remaining names is also what keeps this from
// widening into general-purpose env injection - the ANTHROPIC_ filter already
// applied at the parse boundary bounds what can arrive here at all.
func ApplyEnvFile(ambient []string, envFile map[string]string) []string {
	overlay := make(map[string]string, len(envFile))
	for name, value := range envFile {
		switch name {
		case APIKeyEnvVar, AuthTokenEnvVar:
			continue
		}
		overlay[name] = value
	}
	if len(overlay) == 0 {
		return ambient
	}

	// Overridden names are recorded rather than deleted from the overlay, so a
	// name duplicated in ambient gets the file's value at every position it
	// occupies - otherwise the later, un-overridden entry would win.
	overridden := make(map[string]bool, len(overlay))
	env := make([]string, 0, len(ambient)+len(overlay))
	for _, entry := range ambient {
		name := envName(entry)
		if value, ok := overlay[name]; ok {
			entry = name + "=" + value
			overridden[name] = true
		}
		env = append(env, entry)
	}

	added := make([]string, 0, len(overlay))
	for name, value := range overlay {
		if !overridden[name] {
			added = append(added, name+"="+value)
		}
	}
	sort.Strings(added)

	return append(env, added...)
}

// SubscriptionEnv returns the environment a claude subprocess should run with in
// subscription mode: every inherited variable unchanged, except that both
// APIKeyEnvVar and AuthTokenEnvVar are removed entirely.
//
// Removing rather than blanking them is the point of the mode. The claude CLI
// reads its credential from those variables before falling back to the OAuth
// profile it stores on disk, and an empty value still occupies its slot in that
// precedence chain - it authenticates as an empty credential instead of falling
// through. A direnv .envrc exporting APIKeyEnvVar is the motivating case.
//
// No credential is resolved here, so .utopia/.env is never consulted: a key in
// that file exists for api-key mode, and reading it in this mode would restore
// the credential the mode exists to suppress.
//
// The result is never nil, even for an empty ambient environment. A nil Env
// makes os/exec inherit the parent environment, which would leak the very key
// this mode removes.
func SubscriptionEnv(ambient []string) []string {
	return withoutCredentialVars(ambient, 0)
}

// withoutCredentialVars copies ambient with both Anthropic credential variables
// dropped, reserving room for extra entries the caller appends. Filtering a copy
// is what lets a variable be removed at all - the cmd.Env precedent of appending
// to os.Environ() can only add or shadow, and a shadowing empty value still
// counts as a credential.
func withoutCredentialVars(ambient []string, extra int) []string {
	env := make([]string, 0, len(ambient)+extra)
	for _, entry := range ambient {
		switch envName(entry) {
		case APIKeyEnvVar, AuthTokenEnvVar:
			continue
		}
		env = append(env, entry)
	}
	return env
}

// envName returns the variable name from a "NAME=value" environment entry.
func envName(entry string) string {
	name, _, _ := strings.Cut(entry, "=")
	return name
}

// lookupEnv returns the value of name in a "NAME=value" environment slice, or
// the empty string when it is absent. A later entry wins over an earlier one,
// matching how os/exec resolves a duplicated name.
func lookupEnv(ambient []string, name string) string {
	value := ""
	for _, entry := range ambient {
		if entryName, entryValue, ok := strings.Cut(entry, "="); ok && entryName == name {
			value = entryValue
		}
	}
	return value
}

// MissingAPIKeyError indicates api-key mode was selected but no API key could
// be resolved from any of the locations searched.
type MissingAPIKeyError struct{}

func (e *MissingAPIKeyError) Error() string {
	return fmt.Sprintf("no %s found in %s or %s (required by auth mode %s)",
		APIKeyEnvVar, CredentialSourceEnvFile, CredentialSourceEnvironment, AuthModeAPIKey)
}

// Is allows errors.Is to match any MissingAPIKeyError.
func (e *MissingAPIKeyError) Is(target error) bool {
	_, ok := target.(*MissingAPIKeyError)
	return ok
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
