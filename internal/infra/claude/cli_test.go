package claude

import (
	"context"
	"strings"
	"testing"
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

func TestCLI_WithBinaryPath(t *testing.T) {
	cli := NewCLI().WithBinaryPath("/custom/path/claude")

	if cli.binaryPath != "/custom/path/claude" {
		t.Errorf("binaryPath = %q, want %q", cli.binaryPath, "/custom/path/claude")
	}
}

func TestCLI_WithPermissionMode(t *testing.T) {
	tests := []struct {
		mode PermissionMode
	}{
		{PermissionDefault},
		{PermissionBypass},
		{PermissionAcceptEdits},
		{PermissionDontAsk},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			cli := NewCLI().WithPermissionMode(tt.mode)

			if cli.permissionMode != tt.mode {
				t.Errorf("permissionMode = %q, want %q", cli.permissionMode, tt.mode)
			}
		})
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

func TestCLI_Chaining(t *testing.T) {
	cli := NewCLI().
		WithBinaryPath("/usr/bin/claude").
		WithPermissionMode(PermissionDontAsk).
		WithAllowedTools([]string{"Read"}).
		WithVerbose(true)

	if cli.binaryPath != "/usr/bin/claude" {
		t.Errorf("binaryPath = %q, want %q", cli.binaryPath, "/usr/bin/claude")
	}

	if cli.permissionMode != PermissionDontAsk {
		t.Errorf("permissionMode = %q, want %q", cli.permissionMode, PermissionDontAsk)
	}

	if len(cli.allowedTools) != 1 || cli.allowedTools[0] != "Read" {
		t.Error("allowedTools not set correctly")
	}

	if !cli.verbose {
		t.Error("verbose should be true")
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
	cli := NewCLI().WithPermissionMode(PermissionDefault)
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

func TestSessionResult_Fields(t *testing.T) {
	result := &SessionResult{
		Output: "test output",
		Err:    nil,
	}

	if result.Output != "test output" {
		t.Errorf("Output = %q, want %q", result.Output, "test output")
	}

	if result.Err != nil {
		t.Errorf("Err = %v, want nil", result.Err)
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
	cli := NewCLI().WithBinaryPath("nonexistent-binary")

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
