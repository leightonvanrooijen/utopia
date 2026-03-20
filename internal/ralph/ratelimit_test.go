package ralph

import (
	"strings"
	"testing"
	"time"
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
			name:     "no rate limit",
			stdout:   "Normal output",
			stderr:   "Some error",
			expected: false,
		},
		{
			name:     "partial match - no timezone",
			stdout:   "",
			stderr:   "Limit reached · resets 1am",
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
}
