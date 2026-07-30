package ralph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
)

func TestDetectRateLimit(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		expected bool
	}{
		{
			name:     "rate limit in stderr",
			stdout:   "",
			stderr:   "Limit reached · resets 1am (Australia/Sydney)",
			expected: true,
		},
		{
			name:     "rate limit in stdout",
			stdout:   "Limit reached · resets 1am (Australia/Sydney)",
			stderr:   "",
			expected: true,
		},
		{
			name:     "rate limit with different timezone",
			stdout:   "",
			stderr:   "Limit reached · resets 3pm (America/New_York)",
			expected: true,
		},
		{
			name:     "rate limit with time including minutes",
			stdout:   "",
			stderr:   "Limit reached · resets 12:30pm (Europe/London)",
			expected: true,
		},
		{
			name:     "rate limit with additional context",
			stdout:   "",
			stderr:   "Limit reached · resets 1am (Australia/Sydney) · contact an admin to increase limits",
			expected: true,
		},
		{
			name:     "current session limit format (no timezone)",
			stdout:   "",
			stderr:   "You've hit your session limit · resets 5pm",
			expected: true,
		},
		{
			name:     "current Opus limit format (no timezone)",
			stdout:   "You've hit your Opus limit · resets 3:30pm",
			stderr:   "",
			expected: true,
		},
		{
			name:     "current session limit with timezone",
			stdout:   "",
			stderr:   "You've hit your session limit · resets 5pm (America/New_York)",
			expected: true,
		},
		{
			name:     "no rate limit",
			stdout:   "Normal output",
			stderr:   "Some error",
			expected: false,
		},
		{
			// The timezone is now optional, so a legacy-prefixed message
			// without one is interpreted in system local time and detected.
			name:     "legacy prefix without timezone",
			stdout:   "",
			stderr:   "Limit reached · resets 1am",
			expected: true,
		},
		{
			name:     "spend limit is not a rolling rate limit",
			stdout:   "",
			stderr:   "You've hit your org's monthly spend limit",
			expected: false,
		},
		{
			name:     "empty output",
			stdout:   "",
			stderr:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectRateLimit(tt.stdout, tt.stderr)
			if got != tt.expected {
				t.Errorf("DetectRateLimit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		name        string
		timeStr     string
		expectHour  int
		expectMin   int
		expectError bool
	}{
		{
			name:       "1am",
			timeStr:    "1am",
			expectHour: 1,
			expectMin:  0,
		},
		{
			name:       "12am (midnight)",
			timeStr:    "12am",
			expectHour: 0,
			expectMin:  0,
		},
		{
			name:       "12pm (noon)",
			timeStr:    "12pm",
			expectHour: 12,
			expectMin:  0,
		},
		{
			name:       "1pm",
			timeStr:    "1pm",
			expectHour: 13,
			expectMin:  0,
		},
		{
			name:       "11pm",
			timeStr:    "11pm",
			expectHour: 23,
			expectMin:  0,
		},
		{
			name:       "12:30pm",
			timeStr:    "12:30pm",
			expectHour: 12,
			expectMin:  30,
		},
		{
			name:       "1:45am",
			timeStr:    "1:45am",
			expectHour: 1,
			expectMin:  45,
		},
		{
			name:       "uppercase AM",
			timeStr:    "9AM",
			expectHour: 9,
			expectMin:  0,
		},
		{
			name:        "no suffix",
			timeStr:     "10",
			expectError: true,
		},
		{
			name:        "invalid hour",
			timeStr:     "13am",
			expectError: true,
		},
		{
			name:        "zero hour",
			timeStr:     "0am",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, min, err := parseTimeString(tt.timeStr)
			if tt.expectError {
				if err == nil {
					t.Errorf("parseTimeString(%q) expected error, got none", tt.timeStr)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTimeString(%q) unexpected error: %v", tt.timeStr, err)
				return
			}
			if hour != tt.expectHour || min != tt.expectMin {
				t.Errorf("parseTimeString(%q) = (%d, %d), want (%d, %d)",
					tt.timeStr, hour, min, tt.expectHour, tt.expectMin)
			}
		})
	}
}

func TestParseRateLimitWait(t *testing.T) {
	tests := []struct {
		name        string
		stdout      string
		stderr      string
		expectError bool
		checkDur    func(time.Duration) bool
	}{
		{
			name:   "valid rate limit message",
			stdout: "",
			stderr: "Limit reached · resets 1am (Australia/Sydney)",
			// Can't check exact duration as it depends on current time
			// Just verify it returns a positive duration
			checkDur: func(d time.Duration) bool {
				return d > 0 && d < 25*time.Hour // Should be less than a day + buffer
			},
		},
		{
			name:        "no rate limit pattern",
			stdout:      "normal output",
			stderr:      "normal error",
			expectError: true,
			checkDur: func(d time.Duration) bool {
				return d == DefaultRateLimitWait
			},
		},
		{
			name:        "invalid timezone",
			stdout:      "",
			stderr:      "Limit reached · resets 1am (Invalid/Timezone)",
			expectError: true,
			checkDur: func(d time.Duration) bool {
				return d == DefaultRateLimitWait
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dur, err := ParseRateLimitWait(tt.stdout, tt.stderr)
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseRateLimitWait() expected error, got none")
				}
			}
			if tt.checkDur != nil && !tt.checkDur(dur) {
				t.Errorf("ParseRateLimitWait() duration check failed, got %v", dur)
			}
		})
	}
}

func TestFormatWaitMessage(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		contains []string
	}{
		{
			name:   "valid rate limit",
			stdout: "",
			stderr: "Limit reached · resets 1am (Australia/Sydney)",
			contains: []string{
				"Rate limit hit",
				"Australia/Sydney",
				"waiting until",
			},
		},
		{
			name:   "no rate limit pattern",
			stdout: "normal",
			stderr: "output",
			contains: []string{
				"Rate limit hit",
				DefaultRateLimitWait.String(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := FormatWaitMessage(tt.stdout, tt.stderr)
			for _, substr := range tt.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("FormatWaitMessage() = %q, should contain %q", msg, substr)
				}
			}
		})
	}
}

func TestRateLimitPatternVariations(t *testing.T) {
	// Test that the pattern matches various real-world formats
	validPatterns := []string{
		"Limit reached · resets 1am (Australia/Sydney)",
		"Limit reached · resets 12pm (America/New_York)",
		"Limit reached · resets 3:30pm (Europe/London)",
		"Limit reached · resets 11:59pm (Asia/Tokyo)",
		"Some prefix Limit reached · resets 1am (UTC) some suffix",
		"Limit reached · resets 1am (Australia/Sydney) · contact an admin to increase limits",
		"You've hit your session limit · resets 5pm",
		"You've hit your Opus limit · resets 3:30pm",
		"You've hit your session limit · resets 5pm (America/New_York)",
	}

	for _, pattern := range validPatterns {
		t.Run(pattern, func(t *testing.T) {
			if !DetectRateLimit("", pattern) {
				t.Errorf("Pattern should be detected: %s", pattern)
			}
		})
	}

	invalidPatterns := []string{
		"Limit reached · resets soon",
		"Rate limit exceeded",
		"resets 1am (Australia/Sydney)", // Missing "Limit reached"
		"",
	}

	for _, pattern := range invalidPatterns {
		t.Run("invalid: "+pattern, func(t *testing.T) {
			if DetectRateLimit("", pattern) {
				t.Errorf("Pattern should NOT be detected: %s", pattern)
			}
		})
	}
}

func TestDefaultConstants(t *testing.T) {
	// Verify the constants are set to expected values
	if DefaultRateLimitWait != 10*time.Minute {
		t.Errorf("DefaultRateLimitWait = %v, want 10m", DefaultRateLimitWait)
	}

	if RateLimitBuffer != 2*time.Minute {
		t.Errorf("RateLimitBuffer = %v, want 2m", RateLimitBuffer)
	}

	if ProbeInterval != 30*time.Minute {
		t.Errorf("ProbeInterval = %v, want 30m", ProbeInterval)
	}
}

func TestRollingLimitWithoutTimezoneParses(t *testing.T) {
	// Current message formats omit the timezone. Parsing must succeed by
	// interpreting the reset time in the system local timezone (no error).
	dur, err := ParseRateLimitWait("", "You've hit your session limit · resets 5pm")
	if err != nil {
		t.Fatalf("ParseRateLimitWait() with no timezone returned error: %v", err)
	}
	if dur <= 0 || dur > 25*time.Hour {
		t.Errorf("ParseRateLimitWait() = %v, want a positive duration under ~1 day", dur)
	}

	// The wait message should still be produced without a parse error.
	msg := FormatWaitMessage("", "You've hit your Opus limit · resets 3:30pm")
	if !strings.Contains(msg, "Rate limit hit") || strings.Contains(msg, "parse error") {
		t.Errorf("FormatWaitMessage() = %q, want a clean rate-limit message", msg)
	}
}

func TestDetectSpendLimit(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		expected bool
	}{
		{
			name:     "spend limit in stderr",
			stderr:   "You've hit your org's monthly spend limit",
			expected: true,
		},
		{
			name:     "spend limit in stdout",
			stdout:   "You've hit your org's monthly spend limit · contact an admin",
			expected: true,
		},
		{
			name:     "rolling rate limit is not a spend limit",
			stderr:   "Limit reached · resets 1am (Australia/Sydney)",
			expected: false,
		},
		{
			name:     "no limit",
			stdout:   "normal output",
			stderr:   "normal error",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectSpendLimit(tt.stdout, tt.stderr); got != tt.expected {
				t.Errorf("DetectSpendLimit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestProbeUntilRecovered(t *testing.T) {
	spendLimited := &internal.PromptResult{Stderr: "You've hit your org's monthly spend limit"}

	t.Run("recovers on first successful probe", func(t *testing.T) {
		calls := 0
		probe := func(context.Context) (*internal.PromptResult, error) {
			calls++
			return &internal.PromptResult{Stdout: "ok"}, nil
		}
		if err := probeUntilRecovered(context.Background(), probe, time.Millisecond); err != nil {
			t.Fatalf("probeUntilRecovered() error = %v, want nil", err)
		}
		if calls != 1 {
			t.Errorf("probe called %d times, want 1", calls)
		}
	})

	t.Run("keeps probing while still limited then recovers", func(t *testing.T) {
		calls := 0
		probe := func(context.Context) (*internal.PromptResult, error) {
			calls++
			if calls < 3 {
				// Still limited: the probe itself hits the spend limit and errors.
				return spendLimited, fmt.Errorf("claude prompt failed: exit status 1")
			}
			return &internal.PromptResult{Stdout: "ok"}, nil
		}
		if err := probeUntilRecovered(context.Background(), probe, time.Millisecond); err != nil {
			t.Fatalf("probeUntilRecovered() error = %v, want nil", err)
		}
		if calls != 3 {
			t.Errorf("probe called %d times, want 3", calls)
		}
	})

	t.Run("cancelled during wait returns context error without probing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		probe := func(context.Context) (*internal.PromptResult, error) {
			calls++
			return &internal.PromptResult{Stdout: "ok"}, nil
		}
		// A long interval ensures the cancelled ctx wins the select, exercising
		// the Ctrl+C / timeout graceful-shutdown path.
		err := probeUntilRecovered(ctx, probe, time.Hour)
		if err != context.Canceled {
			t.Errorf("probeUntilRecovered() error = %v, want context.Canceled", err)
		}
		if calls != 0 {
			t.Errorf("probe called %d times, want 0 (cancelled before probing)", calls)
		}
	})
}

func TestHandleClaudeLimits(t *testing.T) {
	t.Run("no limit returns limitNone", func(t *testing.T) {
		result := &internal.PromptResult{Stdout: "all good"}
		outcome, err := handleClaudeLimits(context.Background(), result, "", "")
		if err != nil {
			t.Fatalf("handleClaudeLimits() error = %v, want nil", err)
		}
		if outcome != limitNone {
			t.Errorf("handleClaudeLimits() outcome = %v, want limitNone", outcome)
		}
	})

	t.Run("nil result returns limitNone", func(t *testing.T) {
		outcome, err := handleClaudeLimits(context.Background(), nil, "", "")
		if err != nil || outcome != limitNone {
			t.Errorf("handleClaudeLimits(nil) = (%v, %v), want (limitNone, nil)", outcome, err)
		}
	})

	t.Run("rolling limit with cancelled ctx returns limitWaited and ctx error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := &internal.PromptResult{Stderr: "Limit reached · resets 1am (Australia/Sydney)"}
		outcome, err := handleClaudeLimits(ctx, result, "", "")
		if outcome != limitWaited {
			t.Errorf("handleClaudeLimits() outcome = %v, want limitWaited", outcome)
		}
		if err != context.Canceled {
			t.Errorf("handleClaudeLimits() error = %v, want context.Canceled", err)
		}
	})
}

func TestDetectTurnExhaustion(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		expected bool
	}{
		{
			name:     "observed marker on stdout",
			stdout:   TurnExhaustionMarker,
			expected: true,
		},
		{
			name:     "marker on stderr",
			stderr:   "Error: Reached max turns (40)",
			expected: true,
		},
		{
			name:     "marker after other output",
			stdout:   "Reading files...\nError: Reached max turns (40)",
			expected: true,
		},
		{
			name:     "reworded to maximum",
			stdout:   "Reached maximum turns (40)",
			expected: true,
		},
		{
			name:     "reworded case and spacing",
			stdout:   "reached  max   turns (40)",
			expected: true,
		},
		{
			name:     "ordinary output",
			stdout:   "Done. <COMPLETE>",
			expected: false,
		},
		{
			name:     "a different non-zero exit is not turn exhaustion",
			stderr:   "Error: invalid API key",
			expected: false,
		},
		{
			name:     "the rolling usage limit is not turn exhaustion",
			stderr:   "Limit reached · resets 1am (Australia/Sydney)",
			expected: false,
		},
		{
			name:     "the spend limit is not turn exhaustion",
			stderr:   "You've hit your org's monthly spend limit",
			expected: false,
		},
		{
			name:     "empty output",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectTurnExhaustion(tt.stdout, tt.stderr); got != tt.expected {
				t.Errorf("DetectTurnExhaustion(%q, %q) = %v, want %v", tt.stdout, tt.stderr, got, tt.expected)
			}
		})
	}
}

// TestTurnExhaustionMarkerStillMatches pins the detection to the message the
// Claude CLI was actually observed printing. If a CLI version reworded it past
// what the pattern tolerates, this fails loudly here rather than silently
// reclassifying every turn-capped iteration as a Claude invocation failure.
func TestTurnExhaustionMarkerStillMatches(t *testing.T) {
	if !DetectTurnExhaustion(TurnExhaustionMarker, "") {
		t.Fatalf("DetectTurnExhaustion no longer matches the observed marker %q - "+
			"turn exhaustion would be misreported as an invocation failure", TurnExhaustionMarker)
	}
}
