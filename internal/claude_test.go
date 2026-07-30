package internal

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestNewCLI(t *testing.T) {
	cli := NewCLI()

	if cli == nil {
		t.Fatal("NewCLI() returned nil")
	}

	if cli.binaryPath != "claude" {
		t.Errorf("binaryPath = %q, want %q", cli.binaryPath, "claude")
	}

	if cli.permissionMode != PermissionBypass {
		t.Errorf("permissionMode = %q, want %q", cli.permissionMode, PermissionBypass)
	}

	if cli.verbose {
		t.Error("verbose should default to false")
	}
}

func TestCLI_WithAllowedTools(t *testing.T) {
	tools := []string{"Read", "Write", "Bash"}
	cli := NewCLI().WithAllowedTools(tools)

	if len(cli.allowedTools) != 3 {
		t.Errorf("allowedTools length = %d, want 3", len(cli.allowedTools))
	}

	for i, tool := range tools {
		if cli.allowedTools[i] != tool {
			t.Errorf("allowedTools[%d] = %q, want %q", i, cli.allowedTools[i], tool)
		}
	}
}

func TestCLI_WithVerbose(t *testing.T) {
	cli := NewCLI().WithVerbose(true)

	if !cli.verbose {
		t.Error("verbose should be true")
	}

	cli = cli.WithVerbose(false)

	if cli.verbose {
		t.Error("verbose should be false")
	}
}

func TestCLI_WithModel(t *testing.T) {
	cli := NewCLI().WithModel("opus")

	if cli.model != "opus" {
		t.Errorf("model = %q, want %q", cli.model, "opus")
	}
}

func TestCLI_WithModel_Empty(t *testing.T) {
	cli := NewCLI()

	if cli.model != "" {
		t.Errorf("model should default to empty string, got %q", cli.model)
	}
}

func TestCLI_WithAuth(t *testing.T) {
	cli := NewCLI().WithAuth(domain.AuthModeSubscription, "/project/.utopia")

	if cli.authMode != domain.AuthModeSubscription {
		t.Errorf("authMode = %q, want %q", cli.authMode, domain.AuthModeSubscription)
	}

	if cli.utopiaDir != "/project/.utopia" {
		t.Errorf("utopiaDir = %q, want %q", cli.utopiaDir, "/project/.utopia")
	}
}

func TestCLI_WithAuth_Default(t *testing.T) {
	cli := NewCLI()

	if cli.authMode != "" {
		t.Errorf("authMode should default to the empty mode, got %q", cli.authMode)
	}
}

func TestCLI_Chaining(t *testing.T) {
	cli := NewCLI().
		WithAllowedTools([]string{"Read"}).
		WithVerbose(true).
		WithModel("opus")

	if len(cli.allowedTools) != 1 || cli.allowedTools[0] != "Read" {
		t.Error("allowedTools not set correctly")
	}

	if !cli.verbose {
		t.Error("verbose should be true")
	}

	if cli.model != "opus" {
		t.Errorf("model = %q, want %q", cli.model, "opus")
	}
}

func TestCLI_baseArgs_Default(t *testing.T) {
	cli := NewCLI()
	args := cli.baseArgs()

	// Default has PermissionBypass
	found := false
	for i, arg := range args {
		if arg == "--permission-mode" && i+1 < len(args) && args[i+1] == string(PermissionBypass) {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("baseArgs should include --permission-mode %s, got %v", PermissionBypass, args)
	}
}

func TestCLI_baseArgs_PermissionDefault(t *testing.T) {
	cli := NewCLI()
	cli.permissionMode = PermissionDefault
	args := cli.baseArgs()

	// PermissionDefault should NOT add --permission-mode flag
	for _, arg := range args {
		if arg == "--permission-mode" {
			t.Error("baseArgs should not include --permission-mode for PermissionDefault")
		}
	}
}

func TestCLI_baseArgs_WithAllowedTools(t *testing.T) {
	cli := NewCLI().WithAllowedTools([]string{"Read", "Write", "Bash"})
	args := cli.baseArgs()

	found := false
	for i, arg := range args {
		if arg == "--allowedTools" && i+1 < len(args) {
			if args[i+1] == "Read,Write,Bash" {
				found = true
			}
			break
		}
	}

	if !found {
		t.Errorf("baseArgs should include --allowedTools Read,Write,Bash, got %v", args)
	}
}

func TestCLI_baseArgs_EmptyAllowedTools(t *testing.T) {
	cli := NewCLI().WithAllowedTools([]string{})
	args := cli.baseArgs()

	for _, arg := range args {
		if arg == "--allowedTools" {
			t.Error("baseArgs should not include --allowedTools for empty tools list")
		}
	}
}

func TestCLI_baseArgs_WithModel(t *testing.T) {
	cli := NewCLI().WithModel("opus")
	args := cli.baseArgs()

	found := false
	for i, arg := range args {
		if arg == "--model" && i+1 < len(args) {
			if args[i+1] == "opus" {
				found = true
			}
			break
		}
	}

	if !found {
		t.Errorf("baseArgs should include --model opus, got %v", args)
	}
}

func TestCLI_baseArgs_EmptyModel(t *testing.T) {
	cli := NewCLI()
	args := cli.baseArgs()

	for _, arg := range args {
		if arg == "--model" {
			t.Error("baseArgs should not include --model for empty model")
		}
	}
}

func TestCLI_baseArgs_WithEffort(t *testing.T) {
	cli := NewCLI().WithEffort("medium")
	args := cli.baseArgs()

	found := false
	for i, arg := range args {
		if arg == "--effort" && i+1 < len(args) {
			if args[i+1] == "medium" {
				found = true
			}
			break
		}
	}

	if !found {
		t.Errorf("baseArgs should include --effort medium, got %v", args)
	}
}

func TestCLI_baseArgs_EmptyEffort(t *testing.T) {
	cli := NewCLI()
	args := cli.baseArgs()

	for _, arg := range args {
		if arg == "--effort" {
			t.Error("baseArgs should not include --effort for empty effort, which leaves the CLI on its own default")
		}
	}
}

// Model and effort are separate levers and travel together on one invocation.
func TestCLI_baseArgs_ModelAndEffort(t *testing.T) {
	args := NewCLI().WithModel("opus").WithEffort("high").baseArgs()

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model opus") {
		t.Errorf("baseArgs = %v, want --model opus", args)
	}
	if !strings.Contains(joined, "--effort high") {
		t.Errorf("baseArgs = %v, want --effort high", args)
	}
}

// A cloned CLI carries the effort it was given, so an escalated attempt can vary
// model and effort for one work item without touching the shared instance.
func TestCLI_Clone_CarriesEffort(t *testing.T) {
	base := NewCLI().WithEffort("medium")
	escalated := base.Clone().WithEffort("high")

	if base.effort != "medium" {
		t.Errorf("base effort = %q, want medium - Clone must not mutate the shared instance", base.effort)
	}
	if escalated.effort != "high" {
		t.Errorf("cloned effort = %q, want high", escalated.effort)
	}
}

// argRecordingClaude installs a stand-in claude binary that records the
// arguments it was spawned with, and returns a CLI pointed at it plus a reader
// for that recording. Spawning a real process is what makes the assertion
// meaningful: only the child can report the flags it actually received.
func argRecordingClaude(t *testing.T) (*CLI, func() string) {
	t.Helper()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "claude")
	argsPath := filepath.Join(dir, "args")

	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\n"
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}

	cli := NewCLI()
	cli.binaryPath = binaryPath

	return cli, func() string {
		t.Helper()

		data, err := os.ReadFile(argsPath)
		if err != nil {
			t.Fatalf("fake claude did not record its arguments: %v", err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
}

// Both entry points spawn the binary through baseArgs, so both hand it the
// model and the effort the caller resolved.
func TestCLI_ModelAndEffortReachTheBinary(t *testing.T) {
	t.Run("Prompt", func(t *testing.T) {
		cli, recorded := argRecordingClaude(t)

		if _, err := cli.WithModel("opus").WithEffort("high").Prompt(context.Background(), "hello"); err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}

		args := recorded()
		if !strings.Contains(args, "--model opus") || !strings.Contains(args, "--effort high") {
			t.Errorf("claude args = %q, want --model opus and --effort high", args)
		}
	})

	t.Run("SessionWithCapture", func(t *testing.T) {
		cli, recorded := argRecordingClaude(t)

		// The transcript is empty because the fake writes no session file; the
		// arguments it was given are what this test is about.
		if _, err := cli.WithModel("opus").WithEffort("high").SessionWithCapture(context.Background(), "be brief"); err != nil {
			t.Fatalf("SessionWithCapture() error = %v", err)
		}

		args := recorded()
		if !strings.Contains(args, "--model opus") || !strings.Contains(args, "--effort high") {
			t.Errorf("claude args = %q, want --model opus and --effort high", args)
		}
	})

	// No effort resolved means no flag, leaving the claude CLI on its own default.
	t.Run("no effort omits the flag", func(t *testing.T) {
		cli, recorded := argRecordingClaude(t)

		if _, err := cli.WithModel("opus").Prompt(context.Background(), "hello"); err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}

		if args := recorded(); strings.Contains(args, "--effort") {
			t.Errorf("claude args = %q, want no --effort flag", args)
		}
	})
}

func TestPermissionMode_Constants(t *testing.T) {
	tests := []struct {
		mode     PermissionMode
		expected string
	}{
		{PermissionDefault, "default"},
		{PermissionBypass, "bypassPermissions"},
		{PermissionAcceptEdits, "acceptEdits"},
		{PermissionDontAsk, "dontAsk"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.mode) != tt.expected {
				t.Errorf("got %q, want %q", tt.mode, tt.expected)
			}
		})
	}
}

// Integration-style tests that verify command construction
// These don't actually run Claude but verify args are built correctly

func TestCLI_Prompt_VerboseFlag(t *testing.T) {
	// We can't easily test the actual execution without mocking,
	// but we can verify the verbose flag affects behavior by checking
	// that the CLI is configured correctly

	cli := NewCLI().WithVerbose(true)

	if !cli.verbose {
		t.Error("CLI should have verbose enabled")
	}

	// The Prompt method will use streamingPrompt when verbose is true
	// This is tested by the method structure, not execution
}

func TestCLI_Prompt_NonVerbose(t *testing.T) {
	cli := NewCLI().WithVerbose(false)

	if cli.verbose {
		t.Error("CLI should have verbose disabled")
	}
}

// Test context cancellation handling
func TestCLI_Prompt_ContextCancellation(t *testing.T) {
	cli := NewCLI()
	cli.binaryPath = "nonexistent-binary"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := cli.Prompt(ctx, "test prompt")

	// Should fail (either context cancelled or binary not found)
	if err == nil {
		t.Error("Prompt should fail with cancelled context or missing binary")
	}
}

// Test that verbose streaming captures output correctly (unit test for the builder pattern)
func TestCLI_VerboseOutputBuilder(t *testing.T) {
	// This tests the strings.Builder pattern used in streamingPrompt
	var builder strings.Builder

	lines := []string{"line 1\n", "line 2\n", "line 3\n"}
	for _, line := range lines {
		builder.WriteString(line)
	}

	result := builder.String()
	expected := "line 1\nline 2\nline 3\n"

	if result != expected {
		t.Errorf("builder result = %q, want %q", result, expected)
	}
}

func TestParseSessionJSONL(t *testing.T) {
	// Sample JSONL data mimicking Claude's session storage format
	jsonl := `{"type":"summary","summary":"Test Session"}
{"type":"user","message":{"role":"user","content":"Hello Claude"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello! How can I help you today?"}]}}
{"type":"user","message":{"role":"user","content":"Read a file please"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/test/file.txt"}}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Here is the file content."}]}}
`

	transcript, err := parseSessionJSONL(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("parseSessionJSONL failed: %v", err)
	}

	// Verify user messages are captured
	if !strings.Contains(transcript, "## User") {
		t.Error("transcript should contain '## User' headers")
	}

	if !strings.Contains(transcript, "Hello Claude") {
		t.Error("transcript should contain user message 'Hello Claude'")
	}

	// Verify assistant messages are captured
	if !strings.Contains(transcript, "## Assistant") {
		t.Error("transcript should contain '## Assistant' headers")
	}

	if !strings.Contains(transcript, "Hello! How can I help you today?") {
		t.Error("transcript should contain assistant response")
	}

	// Verify tool calls are captured
	if !strings.Contains(transcript, "[Tool: Read]") {
		t.Error("transcript should contain tool call '[Tool: Read]'")
	}

	// Verify no ANSI codes are present
	if strings.Contains(transcript, "\x1b[") || strings.Contains(transcript, "\033[") {
		t.Error("transcript should not contain ANSI escape codes")
	}
}

func TestParseSessionJSONL_EmptyInput(t *testing.T) {
	transcript, err := parseSessionJSONL(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parseSessionJSONL failed on empty input: %v", err)
	}

	if transcript != "" {
		t.Errorf("expected empty transcript, got %q", transcript)
	}
}

func TestParseSessionJSONL_SkipsNonMessageTypes(t *testing.T) {
	// Include queue-operation and summary types that should be skipped
	jsonl := `{"type":"queue-operation","operation":"enqueue"}
{"type":"summary","summary":"Test"}
{"type":"user","message":{"role":"user","content":"Hello"}}
`

	transcript, err := parseSessionJSONL(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("parseSessionJSONL failed: %v", err)
	}

	if !strings.Contains(transcript, "Hello") {
		t.Error("transcript should contain user message")
	}

	if strings.Contains(transcript, "queue-operation") || strings.Contains(transcript, "enqueue") {
		t.Error("transcript should not contain queue-operation data")
	}
}

func TestExtractUserContent_StringContent(t *testing.T) {
	raw := []byte(`"Hello world"`)
	content := extractUserContent(raw)

	if content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", content)
	}
}

func TestExtractUserContent_NestedJSON(t *testing.T) {
	// This is the format when system prompt injection wraps the message
	raw := []byte(`"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"Actual message\"}}\n"`)
	content := extractUserContent(raw)

	if content != "Actual message" {
		t.Errorf("expected 'Actual message', got %q", content)
	}
}

func TestExtractAssistantContent_TextBlocks(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"Hello"},{"type":"text","text":"World"}]`)
	blocks := extractAssistantContent(raw)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	if blocks[0] != "Hello" || blocks[1] != "World" {
		t.Errorf("unexpected blocks: %v", blocks)
	}
}

func TestExtractAssistantContent_ToolUse(t *testing.T) {
	raw := []byte(`[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]`)
	blocks := extractAssistantContent(raw)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	if blocks[0] != "[Tool: Bash]" {
		t.Errorf("expected '[Tool: Bash]', got %q", blocks[0])
	}
}

func TestExtractAssistantContent_MixedContent(t *testing.T) {
	raw := []byte(`[{"type":"text","text":"Let me read that file"},{"type":"tool_use","name":"Read","input":{"file_path":"/test"}}]`)
	blocks := extractAssistantContent(raw)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	if blocks[0] != "Let me read that file" {
		t.Errorf("expected text content, got %q", blocks[0])
	}

	if blocks[1] != "[Tool: Read]" {
		t.Errorf("expected tool use, got %q", blocks[1])
	}
}

// envVarNames returns the set of variable names in a "NAME=value" slice, which
// is the granularity the backward-compatibility guarantee is stated at: an
// unconfigured run must see the same variables the parent process has.
func envVarNames(env []string) map[string]bool {
	names := make(map[string]bool, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		names[name] = true
	}
	return names
}

func TestCLI_subprocessEnv_NoMode(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "sk-ambient")

	env, err := NewCLI().subprocessEnv()
	if err != nil {
		t.Fatalf("subprocessEnv failed: %v", err)
	}

	got, want := envVarNames(env), envVarNames(os.Environ())
	if !maps.Equal(got, want) {
		t.Errorf("with no auth mode the subprocess environment should hold the same variables as os.Environ(), got %d names, want %d", len(got), len(want))
	}

	if value := lookupTestEnv(env, domain.APIKeyEnvVar); value != "sk-ambient" {
		t.Errorf("%s = %q, want the inherited value unchanged", domain.APIKeyEnvVar, value)
	}
}

func TestCLI_subprocessEnv_Subscription(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "sk-ambient")
	t.Setenv(domain.AuthTokenEnvVar, "token-ambient")

	// The .utopia/.env key must not be consulted in this mode - reading it would
	// restore the credential the mode exists to suppress.
	utopiaDir := writeEnvFile(t, "ANTHROPIC_API_KEY=sk-file\n")

	env, err := NewCLI().WithAuth(domain.AuthModeSubscription, utopiaDir).subprocessEnv()
	if err != nil {
		t.Fatalf("subprocessEnv failed: %v", err)
	}

	for _, name := range []string{domain.APIKeyEnvVar, domain.AuthTokenEnvVar} {
		if envVarNames(env)[name] {
			t.Errorf("%s should be absent in subscription mode", name)
		}
	}
}

func TestCLI_subprocessEnv_APIKey(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "sk-ambient")
	t.Setenv(domain.AuthTokenEnvVar, "token-ambient")

	utopiaDir := writeEnvFile(t, "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_BASE_URL=https://proxy.internal\n")

	env, err := NewCLI().WithAuth(domain.AuthModeAPIKey, utopiaDir).subprocessEnv()
	if err != nil {
		t.Fatalf("subprocessEnv failed: %v", err)
	}

	if value := lookupTestEnv(env, domain.APIKeyEnvVar); value != "sk-file" {
		t.Errorf("%s = %q, want the key from .utopia/.env", domain.APIKeyEnvVar, value)
	}

	if envVarNames(env)[domain.AuthTokenEnvVar] {
		t.Errorf("%s should be absent in api-key mode", domain.AuthTokenEnvVar)
	}

	if value := lookupTestEnv(env, "ANTHROPIC_BASE_URL"); value != "https://proxy.internal" {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want the value from .utopia/.env", value)
	}
}

func TestCLI_subprocessEnv_APIKeyMissing(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "")

	utopiaDir := writeEnvFile(t, "")

	_, err := NewCLI().WithAuth(domain.AuthModeAPIKey, utopiaDir).subprocessEnv()
	if err == nil {
		t.Fatal("subprocessEnv should fail when api-key mode can resolve no key")
	}

	if !errors.Is(err, &domain.MissingAPIKeyError{}) {
		t.Errorf("error %v is not a *MissingAPIKeyError", err)
	}
}

// An unrecognised mode fails rather than falling through to the inherited
// environment: guessing would silently bill the wrong account.
func TestCLI_subprocessEnv_UnknownMode(t *testing.T) {
	_, err := NewCLI().WithAuth(domain.AuthMode("teamplan"), "").subprocessEnv()
	if err == nil {
		t.Fatal("subprocessEnv should fail for an unrecognised auth mode")
	}

	if !errors.Is(err, &domain.InvalidAuthModeError{}) {
		t.Errorf("error %v is not an *InvalidAuthModeError", err)
	}
}

// lookupTestEnv returns the value of name in a "NAME=value" slice.
func lookupTestEnv(env []string, name string) string {
	value := ""
	for _, entry := range env {
		if entryName, entryValue, ok := strings.Cut(entry, "="); ok && entryName == name {
			value = entryValue
		}
	}
	return value
}

// fakeClaude installs a stand-in claude binary that records the environment it
// was spawned with, and returns a CLI pointed at it plus a reader for that
// recording. Spawning a real process is what makes the assertion meaningful: a
// call site that forgets cmd.Env inherits the parent environment silently, and
// only the child can tell us which of the two happened.
func fakeClaude(t *testing.T) (*CLI, func() []string) {
	t.Helper()

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "env.dump")
	binaryPath := filepath.Join(dir, "claude")

	script := "#!/bin/sh\nenv > " + dumpPath + "\n"
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}

	cli := NewCLI()
	cli.binaryPath = binaryPath

	return cli, func() []string {
		t.Helper()

		data, err := os.ReadFile(dumpPath)
		if err != nil {
			t.Fatalf("fake claude did not record its environment: %v", err)
		}
		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
}

// Every way the wrapper spawns claude - a one-shot prompt, a streamed prompt and
// an interactive session - must apply the same credential resolution.
func TestCLI_SpawnSitesApplyCredentialEnv(t *testing.T) {
	sites := []struct {
		name  string
		spawn func(t *testing.T, cli *CLI)
	}{
		{
			name: "Prompt",
			spawn: func(t *testing.T, cli *CLI) {
				if _, err := cli.Prompt(context.Background(), "hello"); err != nil {
					t.Fatalf("Prompt failed: %v", err)
				}
			},
		},
		{
			name: "streamingPrompt",
			spawn: func(t *testing.T, cli *CLI) {
				if _, err := cli.WithVerbose(true).Prompt(context.Background(), "hello"); err != nil {
					t.Fatalf("verbose Prompt failed: %v", err)
				}
			},
		},
		{
			name: "SessionWithCapture",
			spawn: func(t *testing.T, cli *CLI) {
				// The transcript is empty because the fake writes no session file;
				// the environment the process ran with is what is under test.
				if _, err := cli.SessionWithCapture(context.Background(), ""); err != nil {
					t.Fatalf("SessionWithCapture failed: %v", err)
				}
			},
		},
	}

	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			t.Run("subscription suppresses the ambient key", func(t *testing.T) {
				t.Setenv(domain.APIKeyEnvVar, "sk-ambient")

				cli, recorded := fakeClaude(t)
				site.spawn(t, cli.WithAuth(domain.AuthModeSubscription, writeEnvFile(t, "")))

				if envVarNames(recorded())[domain.APIKeyEnvVar] {
					t.Errorf("%s reached the subprocess in subscription mode", domain.APIKeyEnvVar)
				}
			})

			t.Run("api-key injects the resolved key", func(t *testing.T) {
				t.Setenv(domain.APIKeyEnvVar, "sk-ambient")

				cli, recorded := fakeClaude(t)
				utopiaDir := writeEnvFile(t, "ANTHROPIC_API_KEY=sk-file\n")
				site.spawn(t, cli.WithAuth(domain.AuthModeAPIKey, utopiaDir))

				if value := lookupTestEnv(recorded(), domain.APIKeyEnvVar); value != "sk-file" {
					t.Errorf("subprocess %s = %q, want the key from .utopia/.env", domain.APIKeyEnvVar, value)
				}
			})

			t.Run("no mode inherits the ambient environment", func(t *testing.T) {
				t.Setenv(domain.APIKeyEnvVar, "sk-ambient")

				cli, recorded := fakeClaude(t)
				site.spawn(t, cli)

				if value := lookupTestEnv(recorded(), domain.APIKeyEnvVar); value != "sk-ambient" {
					t.Errorf("subprocess %s = %q, want the inherited value", domain.APIKeyEnvVar, value)
				}
			})
		})
	}
}

func TestCLI_baseArgs_WithMaxTurns(t *testing.T) {
	cli := NewCLI().WithMaxTurns(40)
	args := cli.baseArgs()

	found := false
	for i, arg := range args {
		if arg == "--max-turns" && i+1 < len(args) {
			if args[i+1] == "40" {
				found = true
			}
			break
		}
	}

	if !found {
		t.Errorf("baseArgs should include --max-turns 40, got %v", args)
	}
}

func TestCLI_baseArgs_NoMaxTurns(t *testing.T) {
	for _, budget := range []int{0, -1} {
		cli := NewCLI().WithMaxTurns(budget)
		for _, arg := range cli.baseArgs() {
			if arg == "--max-turns" {
				t.Errorf("baseArgs should not include --max-turns for budget %d, got %v", budget, cli.baseArgs())
			}
		}
	}
}

func TestCLI_WithMaxTurns_SurvivesClone(t *testing.T) {
	cli := NewCLI().WithMaxTurns(40)
	clone := cli.Clone().WithModel("opus")

	if clone.maxTurns != 40 {
		t.Errorf("clone.maxTurns = %d, want 40", clone.maxTurns)
	}
	if cli.model != "" {
		t.Errorf("Clone should not mutate the original: model = %q, want empty", cli.model)
	}
}
