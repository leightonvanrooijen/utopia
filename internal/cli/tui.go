package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tuiModel represents the two-pane TUI state
type tuiModel struct {
	width       int
	height      int
	specContent string
	specPath    string

	// Claude process
	claudeCmd    *exec.Cmd
	claudeStdin  io.WriteCloser
	claudeStdout io.ReadCloser

	// Chat content (left pane)
	chatLines []string
	inputLine string

	// Program reference for sending messages from goroutines
	program *tea.Program

	// Channel to signal quit
	quitting bool
}

// Messages for Bubble Tea
type claudeOutputMsg string
type claudeExitMsg struct{ err error }
type specUpdateMsg string
type tickMsg time.Time

// Styles
var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
)

// specSystemPrompt guides Claude through the spec creation workflow
const specSystemPrompt = `You are a Specification Claude - an AI assistant that helps users transform ideas into structured specifications.

## Your Role
Guide users through a natural conversation to create a complete specification. You ask questions, gather requirements, and ultimately produce a structured spec document.

## The Journey (3 Stages)

### STAGE 1: EXPLORE
Help the user articulate their idea:
- What problem are you solving?
- Who is this for? What's their pain?
- What exists today? Why isn't it enough?
- What would success look like?

### STAGE 2: DEFINE
Help scope the project:
- What are the core capabilities?
- What's in scope vs out of scope for v1?
- What are the constraints?
- What are the non-negotiables vs nice-to-haves?

### STAGE 3: SPECIFY
Capture detailed requirements:
- What are the specific features?
- What are the acceptance criteria for each feature?
- What domain knowledge or business rules apply?
- What edge cases should be handled?

## Conversation Guidelines
- Ask ONE question at a time (don't overwhelm)
- Summarize and confirm understanding frequently
- Move naturally between stages as appropriate
- The user can jump between stages - follow their lead
- When you have enough information, offer to generate the spec

## Output Format
When the user is ready, generate the spec in this YAML format and save it using the Write tool:

` + "```yaml" + `
id: kebab-case-identifier
title: Human Readable Title
status: draft
description: |
  Brief description of what this system does.

domain_knowledge:
  - Key business rule or constraint 1
  - Key business rule or constraint 2

features:
  - id: feature-id
    description: What this feature does
    acceptance_criteria:
      - Specific, testable condition 1
      - Specific, testable condition 2
` + "```" + `

## Important
- Be conversational, not robotic
- Extract structure from natural dialogue
- Acceptance criteria must be testable (not vague)
- Ask clarifying questions when requirements are ambiguous
- Encourage the user to think through edge cases

Start by warmly greeting the user and asking what they'd like to build.`

// Additional styles
var (

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	specContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	placeholderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Italic(true)
)

func newTUIModel(specPath string) *tuiModel {
	return &tuiModel{
		specPath:    specPath,
		chatLines:   []string{},
		specContent: "",
	}
}

func (m *tuiModel) Init() tea.Cmd {
	return tea.Batch(
		m.startClaude(),
		m.pollSpecFile(),
	)
}

func (m *tuiModel) startClaude() tea.Cmd {
	return func() tea.Msg {
		args := []string{
			"--permission-mode", "bypassPermissions",
			"--system-prompt", specSystemPrompt,
		}

		m.claudeCmd = exec.Command("claude", args...)

		var err error
		m.claudeStdin, err = m.claudeCmd.StdinPipe()
		if err != nil {
			return claudeExitMsg{err: fmt.Errorf("failed to create stdin pipe: %w", err)}
		}

		m.claudeStdout, err = m.claudeCmd.StdoutPipe()
		if err != nil {
			return claudeExitMsg{err: fmt.Errorf("failed to create stdout pipe: %w", err)}
		}

		// Combine stderr with stdout
		m.claudeCmd.Stderr = m.claudeCmd.Stdout

		if err := m.claudeCmd.Start(); err != nil {
			return claudeExitMsg{err: fmt.Errorf("failed to start claude: %w", err)}
		}

		// Start reading output in background
		go m.readClaudeOutput()

		return nil
	}
}

func (m *tuiModel) readClaudeOutput() {
	reader := bufio.NewReader(m.claudeStdout)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Process ended
				if m.program != nil {
					m.program.Send(claudeExitMsg{err: nil})
				}
				return
			}
			if m.program != nil {
				m.program.Send(claudeExitMsg{err: err})
			}
			return
		}

		// Send output line to TUI via message
		if m.program != nil {
			m.program.Send(claudeOutputMsg(strings.TrimRight(line, "\n\r")))
		}
	}
}

func (m *tuiModel) pollSpecFile() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *tuiModel) readSpecFile() tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(m.specPath)
		if err != nil {
			return specUpdateMsg("")
		}
		return specUpdateMsg(string(content))
	}
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.quitting = true
			if m.claudeCmd != nil && m.claudeCmd.Process != nil {
				m.claudeCmd.Process.Kill()
			}
			return m, tea.Quit

		case tea.KeyEnter:
			if m.claudeStdin != nil && m.inputLine != "" {
				m.chatLines = append(m.chatLines, "> "+m.inputLine)
				io.WriteString(m.claudeStdin, m.inputLine+"\n")
				m.inputLine = ""
			}
			return m, nil

		case tea.KeyBackspace:
			if len(m.inputLine) > 0 {
				m.inputLine = m.inputLine[:len(m.inputLine)-1]
			}
			return m, nil

		default:
			if msg.Type == tea.KeyRunes {
				m.inputLine += string(msg.Runes)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case claudeOutputMsg:
		m.chatLines = append(m.chatLines, string(msg))
		return m, nil

	case claudeExitMsg:
		if msg.err != nil {
			m.chatLines = append(m.chatLines, fmt.Sprintf("[Error: %v]", msg.err))
		} else {
			m.chatLines = append(m.chatLines, "[Session ended]")
		}
		m.quitting = true
		return m, tea.Quit

	case tickMsg:
		// Poll spec file on each tick
		return m, tea.Batch(
			m.readSpecFile(),
			m.pollSpecFile(),
		)

	case specUpdateMsg:
		m.specContent = string(msg)
		return m, nil
	}

	return m, nil
}

func (m *tuiModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Calculate pane dimensions
	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth - 1 // -1 for visual separation
	paneHeight := m.height - 2            // Account for borders

	// Build left pane (Claude chat)
	leftPane := m.renderLeftPane(leftWidth-2, paneHeight-2)

	// Build right pane (spec view)
	rightPane := m.renderRightPane(rightWidth-2, paneHeight-2)

	// Style the panes
	leftStyled := paneStyle.
		Width(leftWidth - 2).
		Height(paneHeight).
		Render(leftPane)

	rightStyled := paneStyle.
		Width(rightWidth - 2).
		Height(paneHeight).
		Render(rightPane)

	// Join panes horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyled, rightStyled)
}

func (m *tuiModel) renderLeftPane(width, height int) string {
	title := titleStyle.Render("Claude Chat")

	lines := m.chatLines

	// Calculate available lines for chat (minus title and input)
	availableLines := height - 3
	if availableLines < 1 {
		availableLines = 1
	}

	// Get the last N lines that fit
	startIdx := 0
	if len(lines) > availableLines {
		startIdx = len(lines) - availableLines
	}
	visibleLines := lines[startIdx:]

	// Build chat content
	chatContent := strings.Join(visibleLines, "\n")

	// Input line with cursor
	inputPrompt := "> " + m.inputLine + "█"

	// Combine
	content := fmt.Sprintf("%s\n\n%s\n\n%s", title, chatContent, inputPrompt)

	return content
}

func (m *tuiModel) renderRightPane(width, height int) string {
	title := titleStyle.Render("Spec File")

	var content string
	if m.specContent == "" {
		content = placeholderStyle.Render("No spec file yet...\n\nThe spec will appear here once Claude creates it.")
	} else {
		// Add line numbers
		lines := strings.Split(m.specContent, "\n")
		numberedLines := make([]string, len(lines))
		for i, line := range lines {
			numberedLines[i] = fmt.Sprintf("%3d │ %s", i+1, line)
		}
		content = specContentStyle.Render(strings.Join(numberedLines, "\n"))
	}

	return fmt.Sprintf("%s\n\n%s", title, content)
}

// RunTUI starts the two-pane TUI for spec creation
func RunTUI(ctx context.Context, specPath string) error {
	m := newTUIModel(specPath)

	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Store program reference so goroutines can send messages
	m.program = p

	_, err := p.Run()
	return err
}
