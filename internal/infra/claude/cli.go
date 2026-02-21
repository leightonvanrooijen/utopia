package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// PermissionMode controls how Claude handles permission prompts
type PermissionMode string

const (
	// PermissionDefault uses standard interactive permission prompts
	PermissionDefault PermissionMode = "default"
	// PermissionBypass skips all permission checks
	PermissionBypass PermissionMode = "bypassPermissions"
	// PermissionAcceptEdits auto-accepts file edits
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	// PermissionDontAsk doesn't ask for permissions
	PermissionDontAsk PermissionMode = "dontAsk"
)

// CLI wraps the Claude CLI binary for orchestration
type CLI struct {
	binaryPath     string
	permissionMode PermissionMode
	allowedTools   []string
	verbose        bool
}

// NewCLI creates a new Claude CLI wrapper with sensible defaults for Utopia
func NewCLI() *CLI {
	return &CLI{
		binaryPath:     "claude",
		permissionMode: PermissionBypass, // Default to no permission prompts
	}
}

// WithBinaryPath sets a custom path to the claude binary
func (c *CLI) WithBinaryPath(path string) *CLI {
	c.binaryPath = path
	return c
}

// WithPermissionMode sets the permission mode
func (c *CLI) WithPermissionMode(mode PermissionMode) *CLI {
	c.permissionMode = mode
	return c
}

// WithAllowedTools sets a whitelist of allowed tools
func (c *CLI) WithAllowedTools(tools []string) *CLI {
	c.allowedTools = tools
	return c
}

// WithVerbose enables verbose output streaming
func (c *CLI) WithVerbose(verbose bool) *CLI {
	c.verbose = verbose
	return c
}

// baseArgs returns common arguments for all Claude invocations
func (c *CLI) baseArgs() []string {
	args := []string{}

	if c.permissionMode != "" && c.permissionMode != PermissionDefault {
		args = append(args, "--permission-mode", string(c.permissionMode))
	}

	if len(c.allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(c.allowedTools, ","))
	}

	return args
}

// SessionResult contains the output from a Claude session
type SessionResult struct {
	Output string
	Err    error
}

// Session runs an interactive Claude session with a system prompt.
// The user interacts directly with Claude CLI, and we capture the output.
func (c *CLI) Session(ctx context.Context, systemPrompt string) (*SessionResult, error) {
	args := c.baseArgs()

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	// Connect stdin/stdout/stderr for interactive session
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("claude session failed: %w", err)
	}

	return &SessionResult{}, nil
}

// Prompt sends a one-shot prompt to Claude and returns the response.
// Uses --print flag for non-interactive output.
// If verbose mode is enabled, streams output in real-time while capturing.
func (c *CLI) Prompt(ctx context.Context, prompt string) (string, error) {
	args := c.baseArgs()
	args = append(args, "--print", prompt)

	// If verbose, use streaming approach
	if c.verbose {
		return c.streamingPrompt(ctx, args)
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude prompt failed: %w", err)
	}

	return string(output), nil
}

// streamingPrompt runs Claude with --verbose and streams output while capturing it.
func (c *CLI) streamingPrompt(ctx context.Context, args []string) (string, error) {
	// Add verbose flag for real-time output
	args = append(args, "--verbose")

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start claude: %w", err)
	}

	// Capture output while streaming to terminal
	var outputBuilder strings.Builder
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				fmt.Print(line) // Stream to terminal
				mu.Lock()
				outputBuilder.WriteString(line)
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}()

	// Stream stderr (verbose output goes here)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderr)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				fmt.Fprint(os.Stderr, line) // Stream to terminal stderr
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for readers to finish
	wg.Wait()

	// Wait for command to complete
	err = cmd.Wait()
	if err != nil {
		return outputBuilder.String(), fmt.Errorf("claude prompt failed: %w", err)
	}

	return outputBuilder.String(), nil
}

// PromptWithSystemPrompt sends a prompt with a custom system prompt
func (c *CLI) PromptWithSystemPrompt(ctx context.Context, systemPrompt, prompt string) (string, error) {
	args := c.baseArgs()
	args = append(args, "--system-prompt", systemPrompt, "--print", prompt)

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude prompt failed: %w", err)
	}

	return string(output), nil
}

// StreamSession runs a Claude session and streams output through a callback.
// This allows Utopia to observe the conversation as it happens.
func (c *CLI) StreamSession(ctx context.Context, systemPrompt string, onOutput func(line string)) error {
	args := c.baseArgs()

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	// Pipe stdout so we can observe it
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
	}

	// Read and forward output
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading output: %w", err)
		}

		// Forward to terminal
		fmt.Print(line)

		// Callback for observation
		if onOutput != nil {
			onOutput(strings.TrimRight(line, "\n"))
		}
	}

	return cmd.Wait()
}

// TODO: RalphLoop - implement Ralph Wiggum loop execution
// This will be implemented when we build the execute strategies
func (c *CLI) RalphLoop(ctx context.Context, prompt string, completionPromise string, maxIterations int) error {
	// Future: invoke /ralph-loop command or set up the loop manually
	return fmt.Errorf("RalphLoop not yet implemented")
}

// SessionWithCapture runs an interactive Claude session and captures the full transcript.
// Reads from Claude's native session storage to get clean transcripts without ANSI codes.
// The transcript is always returned, even if the session fails or is interrupted.
func (c *CLI) SessionWithCapture(ctx context.Context, systemPrompt string) (transcript string, err error) {
	// Generate a unique session ID so we can find the transcript file after
	sessionID := uuid.New().String()

	args := c.baseArgs()
	args = append(args, "--session-id", sessionID)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	// Connect stdin/stdout/stderr directly for full TUI experience
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the interactive session
	cmdErr := cmd.Run()

	// After session ends, read transcript from Claude's session storage
	transcript, readErr := c.readSessionTranscript(sessionID)
	if readErr != nil {
		// If we can't read the transcript, return empty string with the session error
		return "", cmdErr
	}

	return transcript, cmdErr
}

// readSessionTranscript reads and formats a transcript from Claude's session storage.
// Returns a clean transcript with user/assistant messages separated and tool calls captured.
func (c *CLI) readSessionTranscript(sessionID string) (string, error) {
	// Claude stores sessions in ~/.claude/projects/{project-path-encoded}/{session-id}.jsonl
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Get current working directory to find the project folder
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Encode path: replace special characters with dashes to match Claude CLI's encoding
	// Claude replaces "/" and "." with "-"
	encodedPath := strings.ReplaceAll(cwd, "/", "-")
	encodedPath = strings.ReplaceAll(encodedPath, ".", "-")

	sessionFile := filepath.Join(homeDir, ".claude", "projects", encodedPath, sessionID+".jsonl")

	file, err := os.Open(sessionFile)
	if err != nil {
		return "", fmt.Errorf("failed to open session file %s: %w", sessionFile, err)
	}
	defer file.Close()

	return parseSessionJSONL(file)
}

// sessionMessage represents a message from Claude's session storage
type sessionMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // Can be string or array
	} `json:"message"`
	Timestamp string `json:"timestamp"`
}

// contentBlock represents a content block in an assistant message
type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`  // For tool_use
	Input json.RawMessage `json:"input,omitempty"` // For tool_use
}

// parseSessionJSONL parses a Claude session JSONL file and returns a formatted transcript
func parseSessionJSONL(r io.Reader) (string, error) {
	var transcript strings.Builder
	scanner := bufio.NewScanner(r)

	// Increase scanner buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg sessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Skip lines we can't parse (summary, queue-operation, etc.)
			continue
		}

		// Only process user and assistant messages
		if msg.Type != "user" && msg.Type != "assistant" {
			continue
		}

		if msg.Type == "user" {
			content := extractUserContent(msg.Message.Content)
			if content != "" {
				transcript.WriteString("\n## User\n\n")
				transcript.WriteString(content)
				transcript.WriteString("\n")
			}
		} else if msg.Type == "assistant" {
			blocks := extractAssistantContent(msg.Message.Content)
			if len(blocks) > 0 {
				transcript.WriteString("\n## Assistant\n\n")
				for _, block := range blocks {
					transcript.WriteString(block)
					transcript.WriteString("\n")
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return transcript.String(), fmt.Errorf("error scanning session file: %w", err)
	}

	return transcript.String(), nil
}

// extractUserContent extracts text content from a user message
func extractUserContent(raw json.RawMessage) string {
	// Try as string first
	var strContent string
	if err := json.Unmarshal(raw, &strContent); err == nil {
		// Check if it's a JSON-encoded message (from system prompt injection)
		var innerMsg struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(strContent), &innerMsg); err == nil && innerMsg.Message.Content != "" {
			return innerMsg.Message.Content
		}
		return strContent
	}

	// Try as array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

// extractAssistantContent extracts text and tool calls from an assistant message
func extractAssistantContent(raw json.RawMessage) []string {
	var results []string

	// Try as array of content blocks (normal case for assistant)
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					results = append(results, block.Text)
				}
			case "tool_use":
				// Format tool call for readability
				toolCall := fmt.Sprintf("[Tool: %s]", block.Name)
				results = append(results, toolCall)
			}
		}
		return results
	}

	// Fallback: try as string
	var strContent string
	if err := json.Unmarshal(raw, &strContent); err == nil && strContent != "" {
		results = append(results, strContent)
	}

	return results
}
