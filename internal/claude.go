package internal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// PromptResult contains the output from a Claude prompt invocation.
type PromptResult struct {
	// Stdout contains the standard output from Claude
	Stdout string
	// Stderr contains the standard error output from Claude (for rate limit detection, etc.)
	Stderr string
	// Usage is what the invocation spent, read off the claude CLI's terminal result
	// object. It is nil when the caller did not ask for structured output with
	// WithUsageCapture - the roles outside the execution loop - and non-nil with
	// Available false when it was asked for and could not be read.
	Usage *domain.AttemptUsage
}

// GetUsage returns the invocation's usage, nil-safe on the result itself: an
// invocation that failed before producing anything returns no result at all, and
// its caller still has an attempt to record.
func (r *PromptResult) GetUsage() *domain.AttemptUsage {
	if r == nil {
		return nil
	}
	return r.Usage
}

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
	model          string          // model override: alias (e.g. "opus") or full model identifier
	effort         string          // reasoning effort per turn; empty leaves the CLI on its own default
	maxTurns       int             // per-invocation turn ceiling; zero leaves the CLI unbounded
	authMode       domain.AuthMode // credential selection; empty inherits the ambient environment
	utopiaDir      string          // project .utopia dir, where api-key mode looks for .env
	captureUsage   bool            // ask for structured output and read the usage it reports
	streamLevel    slog.Level      // severity of this invocation's live transcript; see WithStreamLevel
	// out receives the subprocess's streamed output. Nil means the process's own
	// streams, which is what an unwired caller always got.
	out *ui.Printer
}

// NewCLI creates a new Claude CLI wrapper with sensible defaults for Utopia
func NewCLI() *CLI {
	return &CLI{
		binaryPath:     "claude",
		permissionMode: PermissionBypass, // Default to no permission prompts
		// A subprocess transcript is deep detail by default: a role invoked in
		// passing - a validator, a sizer, a refinement agent - is watched only by
		// an operator who asked for debug. WithStreamLevel raises it for the one
		// invocation whose transcript is the point of the command.
		streamLevel: slog.LevelDebug,
	}
}

// WithPermissionMode selects how this CLI answers permission prompts. The
// constructor defaults to bypassing them, which is what an executor needs; a
// read-only role sets the default mode instead, so a tool outside its allowlist
// is denied rather than waved through by the bypass.
func (c *CLI) WithPermissionMode(mode PermissionMode) *CLI {
	c.permissionMode = mode
	return c
}

// WithPrinter routes this invocation's streamed subprocess output through the
// caller's printer instead of the process streams, so a command's output stays
// capturable all the way down to what claude itself prints.
func (c *CLI) WithPrinter(p *ui.Printer) *CLI {
	c.out = p
	return c
}

// WithAllowedTools sets a whitelist of allowed tools
func (c *CLI) WithAllowedTools(tools []string) *CLI {
	c.allowedTools = tools
	return c
}

// WithStreamLevel classifies how severe this invocation's live transcript is -
// how loud a run has to be before the operator is shown claude working, rather
// than handed the finished output.
//
// It is a severity, not a gate. Whether the transcript is written is the
// process-wide diagnostic level's decision like every other diagnostic's, so
// there is one threshold and no mechanism gates itself: raising the level to
// debug turns every transcript on, and lowering it to warn turns every
// transcript off, including the execution loop's. What a call site chooses here
// is only where its own transcript sits on that scale - the execution loop's is
// info, because watching the loop work is what `utopia execute` is for, while
// the default (debug) suits a role invoked in passing.
func (c *CLI) WithStreamLevel(level slog.Level) *CLI {
	c.streamLevel = level
	return c
}

// streamsDetail reports whether this invocation streams claude's own output to
// the terminal as it is produced, rather than capturing it and returning it.
//
// Only the stream is gated. The invocation still runs and its output is still
// returned to the caller, so nothing downstream - rate-limit detection, turn
// exhaustion, usage accounting - depends on the level.
func (c *CLI) streamsDetail() bool { return ui.Enabled(c.streamLevel) }

// WithModel sets the model to use for this CLI instance. The value is passed to
// the claude binary's --model flag unchanged: either an alias the CLI resolves to
// the current generation of that model (e.g. "opus") or a full model identifier
// pinning a specific one.
func (c *CLI) WithModel(model string) *CLI {
	c.model = model
	return c
}

// WithEffort sets how much reasoning each turn of this invocation may spend. The
// value is passed to the claude binary's --effort flag unchanged; the empty
// string passes no flag, leaving the CLI on its own default.
//
// Effort is a property of the role being invoked, not of how the invocation is
// going: nothing raises it in response to a failure, a retry or an escalation.
// See domain.EffortConfig and ADR-004.
func (c *CLI) WithEffort(effort string) *CLI {
	c.effort = effort
	return c
}

// WithMaxTurns caps how many turns one invocation of this CLI may spend. The
// value is passed to the claude binary's --max-turns flag; zero or a negative
// value passes no flag, leaving the invocation unbounded, which is the
// behaviour of every call site that does not set a ceiling.
//
// Exhausting the cap is not an error condition of the work: the CLI exits
// non-zero with "Reached max turns (N)" on stdout, and it is the caller's job
// to recognise that as the cap doing what it exists to do. See
// ralph.DetectTurnExhaustion.
func (c *CLI) WithMaxTurns(maxTurns int) *CLI {
	c.maxTurns = maxTurns
	return c
}

// WithUsageCapture asks the claude binary for structured output so the
// invocation's accounting can be read off its terminal result object instead of
// being discarded with the prose. PromptResult.Usage is nil without it.
//
// It is opt-in rather than always-on because only the execution loop's attempts
// are compared against each other. A validator or a discovery call reports no
// usage, and nothing downstream treats that absence as an error - see
// domain.AttemptUsage on the three states a reader has to tell apart.
func (c *CLI) WithUsageCapture(capture bool) *CLI {
	c.captureUsage = capture
	return c
}

// Clone returns a copy of the CLI so a caller can vary one setting for a single
// invocation without mutating the instance every other call site shares. The
// escalation routing uses it to run one work item's attempt on a different model
// while the loop's other work items keep the default executor.
func (c *CLI) Clone() *CLI {
	copied := *c
	return &copied
}

// WithAuth selects the credential every claude subprocess this CLI spawns
// authenticates with. utopiaDir is the project's .utopia directory, which
// api-key mode searches for a .env holding the key.
//
// The zero value - the empty mode - inherits the ambient environment, which is
// the pre-auth behaviour, so the call sites that never set an auth mode keep
// spawning claude exactly as they did before.
func (c *CLI) WithAuth(mode domain.AuthMode, utopiaDir string) *CLI {
	c.authMode = mode
	c.utopiaDir = utopiaDir
	return c
}

// subprocessEnv builds the environment for one claude subprocess from the auth
// mode carried on the CLI. Every exec site calls this, so which credential
// authenticates a run is decided in one place rather than per call site, and a
// one-shot prompt, a streamed prompt and an interactive session all bill to the
// same account.
//
// The selection half of the resolution is discarded here. Reporting which
// credential was chosen belongs to the command, not the subprocess: ralph loops
// until a work item is done and discover fans out over goroutines, so a line
// printed here would repeat per iteration and interleave across goroutines for a
// credential that was only ever chosen once.
func (c *CLI) subprocessEnv() ([]string, error) {
	env, _, err := ResolveAuth(c.authMode, c.utopiaDir, os.Environ())
	return env, err
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

	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	if c.effort != "" {
		args = append(args, "--effort", c.effort)
	}

	if c.maxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(c.maxTurns))
	}

	return args
}

// outputFormatArgs selects the output format for a one-shot prompt. Nothing is
// added without usage capture, which leaves the invocation on the CLI's default
// prose output - the pre-capture behaviour every role outside the execution loop
// keeps.
//
// A streaming run asks for stream-json rather than dropping to the non-streaming
// object, because the operator watching the run is the point of streaming: the
// accounting arrives on the last line of the stream rather than in place of the text.
//
// The claude binary refuses stream-json under --print without --verbose
// ("When using --print, --output-format=stream-json requires --verbose"), so the
// flag travels with the format that makes it mandatory rather than relying on a
// downstream path to add it.
func (c *CLI) outputFormatArgs() []string {
	if !c.captureUsage {
		return nil
	}
	if c.streamsDetail() {
		return []string{"--output-format", "stream-json", "--include-partial-messages", "--verbose"}
	}
	return []string{"--output-format", "json"}
}

// Prompt sends a one-shot prompt to Claude and returns the response.
// Uses --print flag for non-interactive output.
// When the active diagnostic level admits this invocation's stream level,
// streams output in real-time while capturing.
// Returns PromptResult containing both stdout and stderr for rate limit detection,
// and the invocation's usage when the caller asked for it with WithUsageCapture.
func (c *CLI) Prompt(ctx context.Context, prompt string) (*PromptResult, error) {
	args := c.baseArgs()
	args = append(args, c.outputFormatArgs()...)
	args = append(args, "--print", prompt)

	// A run loud enough for this transcript watches the invocation happen;
	// anything quieter waits for it and reads the result.
	if c.streamsDetail() {
		return c.streamingPrompt(ctx, args)
	}

	env, err := c.subprocessEnv()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	cmd.Env = env

	// Capture both stdout and stderr
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	result := &PromptResult{
		Stdout: string(output),
		Stderr: stderr.String(),
	}

	// Read the accounting before the error branch below: an invocation that exited
	// non-zero still spent tokens, and an invocation whose result object could not
	// be parsed is recorded as usage unavailable rather than as a failure.
	if c.captureUsage {
		c.applyJSONUsage(result)
	}

	if err != nil {
		return result, fmt.Errorf("claude prompt failed: %w", err)
	}

	return result, nil
}

// streamingPrompt runs Claude with --verbose and streams output while capturing it.
// Returns PromptResult with both stdout and stderr captured.
func (c *CLI) streamingPrompt(ctx context.Context, args []string) (*PromptResult, error) {
	// Add verbose flag for real-time output, unless the format selection already
	// carried it - the flag must not repeat.
	if !slices.Contains(args, "--verbose") {
		args = append(args, "--verbose")
	}

	env, err := c.subprocessEnv()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	cmd.Env = env

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Capture output while streaming to terminal
	out := ui.OrDefault(c.out)
	var stdoutBuilder strings.Builder
	var stderrBuilder strings.Builder
	var collector streamCollector
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Stream stdout. With usage capture the stream is stream-json, so each line is
	// handed to the collector, which returns the assistant text to print and keeps
	// the accounting - the operator still sees prose appear as it is generated, one
	// delta at a time, rather than waiting for the invocation to finish.
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				mu.Lock()
				display := line
				if c.captureUsage {
					display = collector.consume(line)
				} else {
					stdoutBuilder.WriteString(line)
				}
				mu.Unlock()
				if display != "" {
					out.Print(display) // Stream to terminal
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Stream stderr (verbose output goes here) - now also captures for rate limit detection
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderr)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				out.Progressf("%s", line) // Stream to terminal stderr
				mu.Lock()
				stderrBuilder.WriteString(line)
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for readers to finish
	wg.Wait()

	// Wait for command to complete
	result := &PromptResult{
		Stdout: stdoutBuilder.String(),
		Stderr: stderrBuilder.String(),
	}
	if c.captureUsage {
		collector.apply(c, result)
	}
	err = cmd.Wait()
	if err != nil {
		return result, fmt.Errorf("claude prompt failed: %w", err)
	}

	return result, nil
}

// SessionWithCapture runs an interactive Claude session and captures the full transcript.
// Reads from Claude's native session storage to get clean transcripts without ANSI codes.
// The transcript is always returned, even if the session fails or is interrupted (Ctrl+C).
func (c *CLI) SessionWithCapture(ctx context.Context, systemPrompt string) (transcript string, err error) {
	// Generate a unique session ID so we can find the transcript file after
	sessionID := uuid.New().String()

	args := c.baseArgs()
	args = append(args, "--session-id", sessionID)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	// Resolve before the transcript defer is armed: a credential failure means no
	// session ran, so there is no transcript to capture.
	env, err := c.subprocessEnv()
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	cmd.Env = env

	// Connect stdin/stdout/stderr directly for full TUI experience. This is the
	// one place the process streams are named deliberately rather than routed
	// through a printer: an interactive session needs the real terminal on all
	// three descriptors, and a captured stdout would break the TUI rather than
	// record it.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Use defer to ensure transcript is always captured, even on Ctrl+C or other interrupts.
	// The defer runs after cmd.Run() returns (or panics), capturing whatever was written
	// to Claude's session storage before the interruption.
	defer func() {
		readTranscript, readErr := c.readSessionTranscript(sessionID)
		if readErr == nil {
			transcript = readTranscript
		}
		// If we can't read the transcript, transcript remains empty string
	}()

	// Run the interactive session
	err = cmd.Run()
	return transcript, err
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
