package internal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// writeEnvFile creates a .utopia directory containing the given .env content
// and returns the .utopia path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	if err := os.MkdirAll(utopiaDir, 0o755); err != nil {
		t.Fatalf("failed to create utopia dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(utopiaDir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	return utopiaDir
}

func TestLoadAnthropicEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name:    "reads an anthropic key",
			content: "ANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "ignores names outside the ANTHROPIC_ prefix",
			content: "ANTHROPIC_API_KEY=sk-file\nAWS_SECRET_ACCESS_KEY=nope\nPATH=/evil\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "skips blank lines and comments",
			content: "# the key that pays\n\nANTHROPIC_API_KEY=sk-file\n\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "tolerates an export prefix",
			content: "export ANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "strips double quotes",
			content: "ANTHROPIC_API_KEY=\"sk-file\"\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "strips single quotes",
			content: "ANTHROPIC_API_KEY='sk-file'\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "keeps other anthropic variables",
			content: "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_AUTH_TOKEN=tok\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file", "ANTHROPIC_AUTH_TOKEN": "tok"},
		},
		{
			name:    "ignores a line with no assignment",
			content: "garbage\nANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "keeps an equals sign inside the value",
			content: "ANTHROPIC_API_KEY=sk-a=b\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-a=b"},
		},
		{
			name:    "an empty file defines nothing",
			content: "",
			want:    map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vars, err := LoadAnthropicEnvFile(writeEnvFile(t, tc.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(vars) != len(tc.want) {
				t.Fatalf("loaded %v, want %v", vars, tc.want)
			}
			for name, want := range tc.want {
				if vars[name] != want {
					t.Errorf("%s = %q, want %q", name, vars[name], want)
				}
			}
		})
	}
}

func TestLoadAnthropicEnvFileMissingFileIsNotAnError(t *testing.T) {
	vars, err := LoadAnthropicEnvFile(t.TempDir())

	if err != nil {
		t.Fatalf("a missing .env should not error, got: %v", err)
	}
	if len(vars) != 0 {
		t.Errorf("expected no variables, got %v", vars)
	}
}

func TestResolveAPIKeyEnv(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		ambient    []string
		wantEnv    []string
		wantSource domain.CredentialSource
	}{
		{
			name:       "key from the env file, auth token dropped",
			content:    "ANTHROPIC_API_KEY=sk-file\n",
			ambient:    []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=tok"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			name:       "env file overrides an ambient key",
			content:    "ANTHROPIC_API_KEY=sk-file\n",
			ambient:    []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			name:       "falls back to the ambient key",
			content:    "# no key here\n",
			ambient:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			wantSource: domain.CredentialSourceEnvironment,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, source, err := ResolveAPIKeyEnv(writeEnvFile(t, tc.content), tc.ambient)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
			if len(env) != len(tc.wantEnv) {
				t.Fatalf("env = %v, want %v", env, tc.wantEnv)
			}
			for i := range tc.wantEnv {
				if env[i] != tc.wantEnv[i] {
					t.Errorf("env[%d] = %q, want %q", i, env[i], tc.wantEnv[i])
				}
			}
		})
	}
}

func TestResolveAPIKeyEnvErrorsWhenNoKeyExists(t *testing.T) {
	// Neither location holds a key: the run must fail here, before any claude
	// process is started.
	env, source, err := ResolveAPIKeyEnv(t.TempDir(), []string{"PATH=/usr/bin"})

	if err == nil {
		t.Fatal("expected an error when no key can be resolved")
	}
	var missing *domain.MissingAPIKeyError
	if !errors.As(err, &missing) {
		t.Fatalf("error type = %T, want *domain.MissingAPIKeyError", err)
	}
	if env != nil {
		t.Errorf("expected no environment on failure, got %v", env)
	}
	if source != "" {
		t.Errorf("expected no source on failure, got %q", source)
	}
}

func TestResolveAPIKeyEnvUsesRealProcessEnvironment(t *testing.T) {
	// The direnv case: the key is genuinely exported in this process and
	// os.Environ() is what gets passed through.
	t.Setenv("ANTHROPIC_API_KEY", "sk-exported")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-exported")

	env, source, err := ResolveAPIKeyEnv(t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source != domain.CredentialSourceEnvironment {
		t.Errorf("source = %q, want %q", source, domain.CredentialSourceEnvironment)
	}

	var sawKey bool
	for _, entry := range env {
		if entry == "ANTHROPIC_API_KEY=sk-exported" {
			sawKey = true
		}
		if entry == "ANTHROPIC_AUTH_TOKEN=tok-exported" || entry == "ANTHROPIC_AUTH_TOKEN=" {
			t.Errorf("ANTHROPIC_AUTH_TOKEN should be absent, found %q", entry)
		}
	}
	if !sawKey {
		t.Error("resolved key missing from the subprocess environment")
	}
}
