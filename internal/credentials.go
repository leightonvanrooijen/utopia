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
// is removed, and every other inherited variable is passed through unchanged.
// Pass os.Environ() as ambient.
//
// The key is looked for in .utopia/.env first and the ambient environment
// second. Both locations failing is an error, not a silent unauthenticated run,
// and it surfaces here - before any claude process is started.
func ResolveAPIKeyEnv(utopiaDir string, ambient []string) (env []string, source domain.CredentialSource, err error) {
	envFile, err := LoadAnthropicEnvFile(utopiaDir)
	if err != nil {
		return nil, "", err
	}

	credential, err := domain.ResolveAPIKeyCredential(envFile, ambient)
	if err != nil {
		return nil, "", err
	}

	return credential.Env(ambient), credential.Source, nil
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
