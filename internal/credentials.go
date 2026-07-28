package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// envFileName is the dotenv file inside .utopia that holds Anthropic
// credentials, so the secret lives in a gitignored file rather than in config.
const envFileName = ".env"

// anthropicEnvPrefix limits which names .utopia/.env may contribute. The filter
// sits at the parse boundary so the file cannot become general-purpose
// environment injection into every claude subprocess.
const anthropicEnvPrefix = "ANTHROPIC_"

// ResolveAPIKeyEnv builds the environment for a claude subprocess running in
// api-key mode: the resolved ANTHROPIC_API_KEY is present, ANTHROPIC_AUTH_TOKEN
// is removed, the remaining ANTHROPIC_ variables defined in .utopia/.env are
// applied on top of the inherited ones, and every other inherited variable is
// passed through unchanged. Pass os.Environ() as ambient.
//
// The key is looked for in .utopia/.env first and the ambient environment
// second. Both locations failing is an error, not a silent unauthenticated run,
// and it surfaces here - before any claude process is started.
//
// The file is applied before the credential so the mode has the last word on the
// credential slot: everything else in the file overrides the environment utopia
// inherited, but which credential authenticates the run is decided by the auth
// mode, not by a stray variable in the file.
func ResolveAPIKeyEnv(utopiaDir string, ambient []string) (env []string, source domain.CredentialSource, err error) {
	envFile, err := LoadAnthropicEnvFile(utopiaDir)
	if err != nil {
		return nil, "", err
	}

	credential, err := domain.ResolveAPIKeyCredential(envFile, ambient)
	if err != nil {
		return nil, "", err
	}

	return credential.Env(domain.ApplyEnvFile(ambient, envFile)), credential.Source, nil
}

// ResolveAuth resolves one auth mode into both halves of the same decision: the
// environment a claude subprocess runs with, and the selection a command reports
// before starting one. Pass os.Environ() as ambient.
//
// The two are resolved together on purpose. A report that describes a different
// resolution than the subprocess actually got would be worse than no report at
// all - it would state, with authority, the wrong account. One switch cannot
// drift from itself.
//
// The empty mode returns ambient unchanged and an empty selection: no credential
// was selected, so the subprocess inherits what utopia is running with and there
// is nothing to announce.
//
// A non-empty mode utopia does not recognise is an error rather than a fall
// through to the inherited environment. Both entry points validate the mode
// already, so reaching here with an unknown one is a wiring bug - and the failure
// mode of guessing is a run that silently bills the wrong account, which is the
// outcome this whole feature exists to prevent.
func ResolveAuth(mode domain.AuthMode, utopiaDir string, ambient []string) ([]string, domain.AuthSelection, error) {
	switch mode {
	case "":
		return ambient, domain.AuthSelection{}, nil
	case domain.AuthModeSubscription:
		selection := domain.AuthSelection{
			Mode:       mode,
			Suppressed: domain.SuppressedCredentialVars(ambient),
		}
		return domain.SubscriptionEnv(ambient), selection, nil
	case domain.AuthModeAPIKey:
		env, source, err := ResolveAPIKeyEnv(utopiaDir, ambient)
		if err != nil {
			return nil, domain.AuthSelection{}, fmt.Errorf("failed to resolve credentials for auth mode %s: %w", domain.AuthModeAPIKey, err)
		}
		return env, domain.AuthSelection{Mode: mode, Source: source}, nil
	default:
		return nil, domain.AuthSelection{}, &domain.InvalidAuthModeError{Mode: string(mode)}
	}
}

// ResolveAuthSelection resolves what a run will authenticate with and returns
// only the reportable half, discarding the environment and with it the
// credential value. A caller that wants to name the account that pays never
// holds the secret it is naming.
//
// Resolving here also means an api-key mode run with no key anywhere fails
// before the command does any work, rather than at the first spawn.
func ResolveAuthSelection(mode domain.AuthMode, utopiaDir string, ambient []string) (domain.AuthSelection, error) {
	_, selection, err := ResolveAuth(mode, utopiaDir, ambient)
	return selection, err
}

// LoadAnthropicEnvFile reads the ANTHROPIC_-prefixed variables defined in
// <utopiaDir>/.env. Names outside that prefix are ignored. A missing file is
// not an error and yields a nil map, which reads as "nothing configured here".
func LoadAnthropicEnvFile(utopiaDir string) (map[string]string, error) {
	path := filepath.Join(utopiaDir, envFileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return parseAnthropicEnvFile(string(data)), nil
}

// parseAnthropicEnvFile parses dotenv content, keeping only ANTHROPIC_ names.
// Blank lines and # comments are skipped, an optional "export " prefix is
// tolerated, and a single- or double-quoted value is unquoted. A line without
// an "=" is ignored rather than failing the load, so a hand-edited file with a
// stray word in it still yields the keys it does define.
func parseAnthropicEnvFile(content string) map[string]string {
	vars := make(map[string]string)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, anthropicEnvPrefix) {
			continue
		}

		vars[name] = unquoteEnvValue(strings.TrimSpace(value))
	}

	return vars
}

// unquoteEnvValue strips one layer of matching single or double quotes, which
// dotenv files use to keep trailing spaces or #-containing values intact.
func unquoteEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
		return value[1 : len(value)-1]
	}
	return value
}
