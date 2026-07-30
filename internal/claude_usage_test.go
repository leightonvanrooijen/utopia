package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// jsonResultPayload is a terminal result object shaped like the one Claude Code
// 2.1.x prints for `--print --output-format json`. The resolved model id appears
// only as a modelUsage key, which is why the reader goes looking there.
const jsonResultPayload = `{"is_error":false,"num_turns":7,"duration_ms":91234,"session_id":"abc",` +
	`"total_cost_usd":1.25,"usage":{"input_tokens":11,"output_tokens":222,` +
	`"cache_read_input_tokens":3333,"cache_creation_input_tokens":444},` +
	`"modelUsage":{"claude-opus-5-20260101":{"outputTokens":222,"costUSD":1.25}},` +
	`"subtype":"success","result":"done here <COMPLETE>","type":"result"}`

// scriptedClaude installs a stand-in claude binary running the given shell body,
// and returns a CLI pointed at it. Spawning a real process is what makes the
// assertions meaningful: the parsing runs over bytes that actually came off a
// pipe.
func scriptedClaude(t *testing.T, body string) *CLI {
	t.Helper()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "claude")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}

	cli := NewCLI()
	cli.binaryPath = binaryPath
	return cli
}

// apiKeyUtopiaDir returns a .utopia directory holding a key, so api-key auth
// resolves without reaching for the ambient environment.
func apiKeyUtopiaDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ANTHROPIC_API_KEY=sk-test\n"), 0o600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	return dir
}

// captureStdout swaps os.Stdout for a pipe and returns a reader for what was
// printed to it. The streaming path prints with fmt.Print, so this is what lets a
// test see what the operator would have seen.
func captureStdout(t *testing.T) func() string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	return func() string {
		t.Helper()
		os.Stdout = original
		w.Close()
		var out strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			out.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		r.Close()
		return out.String()
	}
}

// The output-format flag is what makes the accounting available at all, and it is
// asked for only by the callers that opted in - a validator or a discovery call
// keeps the CLI's prose output.
func TestCLI_UsageCapture_RequestsStructuredOutput(t *testing.T) {
	tests := []struct {
		name    string
		capture bool
		verbose bool
		want    []string
		notWant []string
	}{
		{
			name:    "capture off asks for no format",
			notWant: []string{"--output-format"},
		},
		{
			name:    "capture on asks for the result object",
			capture: true,
			want:    []string{"--output-format json"},
			notWant: []string{"stream-json"},
		},
		{
			name:    "capture on and verbose asks for the stream",
			capture: true,
			verbose: true,
			want:    []string{"--output-format stream-json", "--include-partial-messages"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli, recorded := argRecordingClaude(t)
			restore := captureStdout(t)

			_, err := cli.WithUsageCapture(tt.capture).WithVerbose(tt.verbose).Prompt(context.Background(), "hello")
			restore()
			if err != nil {
				t.Fatalf("Prompt() error = %v", err)
			}

			args := recorded()
			for _, want := range tt.want {
				if !strings.Contains(args, want) {
					t.Errorf("claude args = %q, want it to contain %q", args, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(args, notWant) {
					t.Errorf("claude args = %q, want it not to contain %q", args, notWant)
				}
			}
		})
	}
}

// Every field a model comparison needs comes off the result object, and the model
// it is keyed on is the one the CLI resolved - not the alias the loop passed.
func TestCLI_Prompt_ReadsUsageFromResultObject(t *testing.T) {
	cli := scriptedClaude(t, "cat <<'EOF'\n"+jsonResultPayload+"\nEOF")

	result, err := cli.
		WithUsageCapture(true).
		WithModel("opus").
		WithEffort("high").
		WithAuth(domain.AuthModeAPIKey, apiKeyUtopiaDir(t)).
		Prompt(context.Background(), "go")
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	usage := result.GetUsage()
	if !usage.IsAvailable() {
		t.Fatalf("usage = %+v, want available", usage)
	}
	if usage.Model != "claude-opus-5-20260101" {
		t.Errorf("usage.Model = %q, want the resolved id rather than the alias %q", usage.Model, "opus")
	}
	if usage.Effort != "high" {
		t.Errorf("usage.Effort = %q, want high", usage.Effort)
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 222 {
		t.Errorf("tokens = in %d out %d, want in 11 out 222", usage.InputTokens, usage.OutputTokens)
	}
	if usage.CacheReadTokens != 3333 || usage.CacheCreationTokens != 444 {
		t.Errorf("cache tokens = read %d created %d, want read 3333 created 444",
			usage.CacheReadTokens, usage.CacheCreationTokens)
	}
	if usage.Turns != 7 {
		t.Errorf("usage.Turns = %d, want 7", usage.Turns)
	}
	if usage.DurationMS != 91234 {
		t.Errorf("usage.DurationMS = %d, want 91234", usage.DurationMS)
	}
	if usage.CostUSD != 1.25 {
		t.Errorf("usage.CostUSD = %v, want 1.25", usage.CostUSD)
	}

	// The JSON envelope is unwrapped, so the completion-token check and the limit
	// detectors read the same prose they read before structured output was asked for.
	if result.Stdout != "done here <COMPLETE>" {
		t.Errorf("Stdout = %q, want the assistant text", result.Stdout)
	}
}

// The dollar figure carries what it means. Under subscription auth no per-token
// charge is incurred, so the CLI's number is a list-price equivalent; under api-key
// auth it is money.
func TestCLI_Prompt_CostCarriesItsBasis(t *testing.T) {
	tests := []struct {
		mode      domain.AuthMode
		wantBasis domain.CostBasis
		charged   bool
	}{
		{domain.AuthModeAPIKey, domain.CostBasisCharged, true},
		{domain.AuthModeSubscription, domain.CostBasisListPriceEstimate, false},
		{"", domain.CostBasisUnknown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.wantBasis), func(t *testing.T) {
			cli := scriptedClaude(t, "cat <<'EOF'\n"+jsonResultPayload+"\nEOF")

			result, err := cli.WithUsageCapture(true).WithAuth(tt.mode, apiKeyUtopiaDir(t)).Prompt(context.Background(), "go")
			if err != nil {
				t.Fatalf("Prompt() error = %v", err)
			}

			usage := result.GetUsage()
			if usage.CostBasis != tt.wantBasis {
				t.Errorf("usage.CostBasis = %q, want %q", usage.CostBasis, tt.wantBasis)
			}
			if usage.CostIsCharged() != tt.charged {
				t.Errorf("usage.CostIsCharged() = %v, want %v", usage.CostIsCharged(), tt.charged)
			}
		})
	}
}

// Verbose mode is for the operator watching the run, so the assistant's text still
// reaches the terminal as it is generated while the accounting is read off the last
// line of the same stream.
func TestCLI_Prompt_VerboseStreamsTextAndCapturesUsage(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"claude-opus-5-20260101"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"secret reasoning"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"working"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":" <COMPLETE>"}}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":3,"duration_ms":42,` +
			`"total_cost_usd":0.5,"usage":{"input_tokens":1,"output_tokens":2},"result":" <COMPLETE>"}`,
	}, "\n")

	cli := scriptedClaude(t, "cat <<'EOF'\n"+stream+"\nEOF")

	restore := captureStdout(t)
	result, err := cli.WithUsageCapture(true).WithVerbose(true).Prompt(context.Background(), "go")
	printed := restore()
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if !strings.Contains(printed, "working <COMPLETE>") {
		t.Errorf("streamed output = %q, want the assistant text", printed)
	}
	if strings.Contains(printed, "secret reasoning") {
		t.Errorf("streamed output = %q, want no thinking deltas", printed)
	}
	if strings.Contains(printed, `"type":"stream_event"`) {
		t.Errorf("streamed output = %q, want prose rather than the stream envelope", printed)
	}

	usage := result.GetUsage()
	if !usage.IsAvailable() {
		t.Fatalf("usage = %+v, want available", usage)
	}
	// No modelUsage on this result object, so the resolved id comes off the init
	// message - still the resolved id, never the alias.
	if usage.Model != "claude-opus-5-20260101" {
		t.Errorf("usage.Model = %q, want the id from the init message", usage.Model)
	}
	if usage.Turns != 3 || usage.CostUSD != 0.5 {
		t.Errorf("usage = %+v, want 3 turns and 0.5 cost", usage)
	}
	if !strings.Contains(result.Stdout, "<COMPLETE>") {
		t.Errorf("Stdout = %q, want the accumulated assistant text", result.Stdout)
	}
}

// An invocation whose accounting cannot be read is recorded as unavailable and is
// not an error: the work it did stands, and the output the failure detectors read
// is left exactly as the CLI wrote it.
func TestCLI_Prompt_UnparseableUsageIsRecordedNotFailed(t *testing.T) {
	t.Run("prose instead of a result object", func(t *testing.T) {
		cli := scriptedClaude(t, `printf 'just prose <COMPLETE>\n'`)

		result, err := cli.WithUsageCapture(true).Prompt(context.Background(), "go")
		if err != nil {
			t.Fatalf("Prompt() error = %v, want the invocation to succeed", err)
		}

		usage := result.GetUsage()
		if usage == nil {
			t.Fatal("usage = nil, want a record marked unavailable")
		}
		if usage.Available {
			t.Errorf("usage = %+v, want available false", usage)
		}
		if usage.UnavailableReason == "" {
			t.Error("usage.UnavailableReason is empty, want the reason the gap exists")
		}
		if !strings.Contains(result.Stdout, "<COMPLETE>") {
			t.Errorf("Stdout = %q, want the CLI output left intact", result.Stdout)
		}
	})

	// The turn ceiling prints prose and exits non-zero, and the loop classifies it
	// off that prose. Structured output must not swallow it.
	t.Run("turn exhaustion keeps its marker", func(t *testing.T) {
		cli := scriptedClaude(t, `printf 'Error: Reached max turns (1)\n'; exit 1`)

		result, _ := cli.WithUsageCapture(true).Prompt(context.Background(), "go")
		if !strings.Contains(result.Stdout, "Reached max turns") {
			t.Errorf("Stdout = %q, want the turn-exhaustion marker", result.Stdout)
		}
		if result.GetUsage().IsAvailable() {
			t.Errorf("usage = %+v, want unavailable", result.GetUsage())
		}
	})

	t.Run("verbose stream without a result object", func(t *testing.T) {
		cli := scriptedClaude(t, `printf 'Error: something went wrong\n'; exit 1`)

		restore := captureStdout(t)
		result, _ := cli.WithUsageCapture(true).WithVerbose(true).Prompt(context.Background(), "go")
		printed := restore()

		if !strings.Contains(printed, "something went wrong") {
			t.Errorf("streamed output = %q, want non-JSON output shown to the operator", printed)
		}
		if result.GetUsage().IsAvailable() {
			t.Errorf("usage = %+v, want unavailable", result.GetUsage())
		}
		if !strings.Contains(result.Stdout, "something went wrong") {
			t.Errorf("Stdout = %q, want the raw stream", result.Stdout)
		}
	})
}

// Roles outside the execution loop - validators, discovery - report no usage at
// all, and that is a third state: not captured, as against captured and empty.
func TestCLI_Prompt_WithoutCaptureReportsNoUsage(t *testing.T) {
	cli := scriptedClaude(t, `printf 'prose\n'`)

	result, err := cli.Prompt(context.Background(), "go")
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if result.Usage != nil {
		t.Errorf("Usage = %+v, want nil when capture was not asked for", result.Usage)
	}
	if result.Stdout != "prose\n" {
		t.Errorf("Stdout = %q, want the CLI prose unchanged", result.Stdout)
	}
}

// A run that spent tokens on more than one model is attributed to the one that did
// the generating, so a subagent on a cheaper tier does not relabel the attempt.
func TestResolvedModel_PrefersTheModelThatGenerated(t *testing.T) {
	payload := &cliResultPayload{
		ModelUsage: map[string]struct {
			OutputTokens int `json:"outputTokens"`
		}{
			"claude-haiku-4-5-20251001": {OutputTokens: 10},
			"claude-opus-5-20260101":    {OutputTokens: 900},
		},
	}

	if got := payload.resolvedModel("fallback"); got != "claude-opus-5-20260101" {
		t.Errorf("resolvedModel() = %q, want the model with the most output tokens", got)
	}

	empty := &cliResultPayload{}
	if got := empty.resolvedModel("claude-opus-5-20260101"); got != "claude-opus-5-20260101" {
		t.Errorf("resolvedModel() = %q, want the fallback when modelUsage is absent", got)
	}
}
