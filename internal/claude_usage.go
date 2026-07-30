package internal

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Reasons recorded on an attempt whose usage could not be read. They are phrased
// for a person reading a gap in the record, and they are distinct so the gap says
// which of the two happened.
const (
	usageUnavailableNoResult   = "the claude CLI produced no parseable terminal result object"
	usageUnavailableNotCapture = "structured output was not requested for this invocation"
)

// cliResultPayload is the claude CLI's terminal result object, the last thing a
// `--output-format json` run prints and the last line of a
// `--output-format stream-json` run. Only the fields the accounting needs are
// declared; the object carries considerably more.
type cliResultPayload struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	NumTurns     int     `json:"num_turns"`
	DurationMS   int64   `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`

	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`

	// ModelUsage is keyed by the resolved model id - the only place the id appears
	// in a non-streaming run, which is why it is read rather than the alias on the
	// CLI struct.
	ModelUsage map[string]struct {
		OutputTokens int `json:"outputTokens"`
	} `json:"modelUsage"`
}

// resolvedModel returns the model id the CLI actually ran, preferring the keys of
// modelUsage over the fallback (the model reported on a streaming run's init
// message).
//
// More than one key means the run spent tokens on more than one model - a subagent
// on a different tier, for instance. The attempt's model is the one that did the
// bulk of the generating, so the key with the most output tokens wins, ties broken
// by name so the same payload always resolves the same way.
func (p *cliResultPayload) resolvedModel(fallback string) string {
	if len(p.ModelUsage) == 0 {
		return fallback
	}

	ids := make([]string, 0, len(p.ModelUsage))
	for id := range p.ModelUsage {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	best := ids[0]
	for _, id := range ids[1:] {
		if p.ModelUsage[id].OutputTokens > p.ModelUsage[best].OutputTokens {
			best = id
		}
	}
	return best
}

// usage converts the result object into the recorded form. effort and mode come
// from the invocation rather than the payload: the CLI reports neither back.
func (p *cliResultPayload) usage(model, effort string, mode domain.AuthMode) *domain.AttemptUsage {
	return &domain.AttemptUsage{
		Available:           true,
		Model:               model,
		Effort:              effort,
		InputTokens:         p.Usage.InputTokens,
		OutputTokens:        p.Usage.OutputTokens,
		CacheReadTokens:     p.Usage.CacheReadInputTokens,
		CacheCreationTokens: p.Usage.CacheCreationInputTokens,
		Turns:               p.NumTurns,
		DurationMS:          p.DurationMS,
		CostUSD:             p.TotalCostUSD,
		CostBasis:           domain.CostBasisForAuth(mode),
	}
}

// parseJSONResult reads the terminal result object out of a `--output-format json`
// run's stdout. ok is false for anything that is not a result object, which is
// every path where the CLI failed before producing one: an auth error, a usage
// limit notice, the turn ceiling. Those print prose, and the caller keeps that
// prose as the invocation's output so the existing detectors still see it.
func parseJSONResult(stdout string) (*cliResultPayload, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil, false
	}

	var payload cliResultPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	if payload.Type != "result" {
		return nil, false
	}
	return &payload, true
}

// applyJSONUsage folds a non-streaming structured run's output into the result:
// the usage onto Usage, and the assistant's text onto Stdout in place of the JSON
// envelope, so every caller that reads Stdout for the completion token or for a
// limit notice reads what it read before structured output was asked for.
//
// A payload that reports an error keeps the raw output instead. The error text the
// CLI wrote is what the limit and turn-exhaustion detectors match on, and the
// result field of a failed run does not reliably carry it.
func (c *CLI) applyJSONUsage(result *PromptResult) {
	payload, ok := parseJSONResult(result.Stdout)
	if !ok {
		result.Usage = domain.UnavailableUsage(usageUnavailableNoResult)
		return
	}

	result.Usage = payload.usage(payload.resolvedModel(""), c.effort, c.authMode)
	if !payload.IsError && payload.Result != "" {
		result.Stdout = payload.Result
	}
}

// streamCollector reads a `--output-format stream-json` run one line at a time. It
// exists to serve two purposes from one pass: the operator sees assistant text as
// it is generated, and the run's usage is read off the terminal result object at
// the end.
type streamCollector struct {
	// initModel is the resolved model id from the init message, which is the main
	// model of the session.
	initModel string
	// text accumulates every assistant text delta, which is the prose a
	// non-streaming run would have printed.
	text strings.Builder
	// raw keeps the stream verbatim, for the paths where the CLI failed without
	// producing a result object and the caller needs whatever it did write.
	raw strings.Builder
	// result is the terminal result object, nil until it arrives.
	result *cliResultPayload
}

// streamLine is one line of a stream-json run, in the shapes this reader cares
// about: the init message carrying the resolved model, a partial-message event
// carrying a text delta, and the terminal result object.
type streamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Event   struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

// consume records one line and returns the text to show the operator, which is
// empty for every line that is not assistant prose.
//
// A line that is not JSON at all is shown unchanged: the CLI writes plain text on
// some failure paths, and swallowing it would leave the operator watching nothing
// happen.
func (s *streamCollector) consume(line string) string {
	s.raw.WriteString(line)

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	var parsed streamLine
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return line
	}

	switch parsed.Type {
	case "system":
		if parsed.Subtype == "init" && parsed.Model != "" {
			s.initModel = parsed.Model
		}
	case "stream_event":
		// Only text deltas are streamed on. Thinking deltas are the model's
		// reasoning rather than its answer, and the operator watching a run wants the
		// answer; the transcript keeps neither either way.
		if parsed.Event.Type == "content_block_delta" && parsed.Event.Delta.Type == "text_delta" {
			s.text.WriteString(parsed.Event.Delta.Text)
			return parsed.Event.Delta.Text
		}
	case "result":
		var payload cliResultPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			s.result = &payload
		}
	}

	return ""
}

// apply folds what the stream reported onto the result, on the same terms as
// applyJSONUsage: usage if a result object arrived, prose in place of the JSON,
// and the raw stream when the run failed without accounting for itself.
func (s *streamCollector) apply(c *CLI, result *PromptResult) {
	if s.result == nil {
		result.Usage = domain.UnavailableUsage(usageUnavailableNoResult)
		result.Stdout = s.raw.String()
		return
	}

	result.Usage = s.result.usage(s.result.resolvedModel(s.initModel), c.effort, c.authMode)

	// The accumulated deltas are every turn's assistant text, so they are a superset
	// of the result field, which is only the last message. Preferring them keeps a
	// completion token emitted mid-run visible to the caller.
	switch {
	case s.result.IsError:
		result.Stdout = s.raw.String()
	case s.text.Len() > 0:
		result.Stdout = s.text.String()
	case s.result.Result != "":
		result.Stdout = s.result.Result
	default:
		result.Stdout = s.raw.String()
	}
}
