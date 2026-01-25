package claude

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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
func (c *CLI) Prompt(ctx context.Context, prompt string) (string, error) {
	args := c.baseArgs()
	args = append(args, "--print", prompt)

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude prompt failed: %w", err)
	}

	return string(output), nil
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
