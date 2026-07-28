package ralph

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// rateLimitPattern matches the Claude rolling usage-limit message across the
// legacy and current formats. Every rolling-limit message names a reset time;
// the timezone is only present in the legacy format, so it is optional here.
// Recognised prefixes:
// - "Limit reached · resets {time} ({timezone})"   (legacy)
// - "You've hit your session limit · resets {time}" (current)
// - "You've hit your Opus limit · resets {time}"    (current)
// Examples:
// - "Limit reached · resets 1am (Australia/Sydney)"
// - "Limit reached · resets 12:30pm (America/New_York)"
// - "You've hit your session limit · resets 5pm"
// - "You've hit your Opus limit · resets 3:30pm"
// The timezone capture group (matches[2]) is empty when the message omits it,
// in which case the reset time is interpreted in the system local timezone.
var rateLimitPattern = regexp.MustCompile(`(?:Limit reached|You've hit your [^·]*?limit)\s*·\s*resets\s+(\d{1,2}(?::\d{2})?(?:am|pm))(?:\s+\(([^)]+)\))?`)

// RateLimitInfo contains parsed rate limit information
type RateLimitInfo struct {
	// ResetTime is the time when the rate limit resets
	ResetTime time.Time
	// Timezone is the timezone string from the rate limit message
	Timezone string
	// WaitDuration is the calculated duration to wait (including 2 minute buffer)
	WaitDuration time.Duration
}

// DefaultRateLimitWait is the fallback wait duration if time parsing fails
const DefaultRateLimitWait = 10 * time.Minute

// RateLimitBuffer is added to the calculated wait time to ensure the limit has reset
const RateLimitBuffer = 2 * time.Minute

// DetectRateLimit checks if the output contains a rate limit message.
// It checks both stdout and stderr as the message may appear in either.
func DetectRateLimit(stdout, stderr string) bool {
	combined := stdout + "\n" + stderr
	return rateLimitPattern.MatchString(combined)
}

// ParseRateLimitWait parses a rate limit message and returns the duration to wait.
// Returns the wait duration and any error encountered during parsing.
// If parsing fails, returns DefaultRateLimitWait and an error.
func ParseRateLimitWait(stdout, stderr string) (time.Duration, error) {
	combined := stdout + "\n" + stderr

	matches := rateLimitPattern.FindStringSubmatch(combined)
	if matches == nil {
		return DefaultRateLimitWait, fmt.Errorf("no rate limit pattern found in output")
	}

	timeStr := matches[1] // e.g., "1am" or "12:30pm"
	tzStr := matches[2]   // e.g., "Australia/Sydney"

	resetTime, err := parseResetTime(timeStr, tzStr)
	if err != nil {
		return DefaultRateLimitWait, fmt.Errorf("failed to parse reset time: %w", err)
	}

	// Calculate wait duration from now
	now := time.Now()
	waitDuration := resetTime.Sub(now) + RateLimitBuffer

	// If the calculated time is in the past (already passed), add a day
	// This handles cases where the reset time is shortly after midnight
	if waitDuration <= 0 {
		resetTime = resetTime.Add(24 * time.Hour)
		waitDuration = resetTime.Sub(now) + RateLimitBuffer
	}

	return waitDuration, nil
}

// parseResetTime converts a time string and timezone into a time.Time for today or tomorrow.
// An empty timezone means the message omitted one (current message formats), so
// the reset time is interpreted in the system local timezone.
func parseResetTime(timeStr, tzStr string) (time.Time, error) {
	// Load the timezone, defaulting to system local when none was provided.
	loc := time.Local
	if tzStr != "" {
		var err error
		loc, err = time.LoadLocation(tzStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timezone %q: %w", tzStr, err)
		}
	}

	// Parse the time string
	hour, minute, err := parseTimeString(timeStr)
	if err != nil {
		return time.Time{}, err
	}

	// Get current time in the target timezone
	now := time.Now().In(loc)

	// Create the reset time for today in the target timezone
	resetTime := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)

	return resetTime, nil
}

// parseTimeString parses a time string like "1am", "12pm", "12:30pm" into hour and minute.
func parseTimeString(timeStr string) (hour, minute int, err error) {
	timeStr = strings.ToLower(strings.TrimSpace(timeStr))

	isPM := strings.HasSuffix(timeStr, "pm")
	isAM := strings.HasSuffix(timeStr, "am")

	if !isPM && !isAM {
		return 0, 0, fmt.Errorf("time string %q missing am/pm suffix", timeStr)
	}

	// Remove am/pm suffix
	timeStr = strings.TrimSuffix(timeStr, "am")
	timeStr = strings.TrimSuffix(timeStr, "pm")

	// Check for minute component
	if strings.Contains(timeStr, ":") {
		parts := strings.Split(timeStr, ":")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("invalid time format %q", timeStr)
		}
		hour, err = strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid hour in %q: %w", timeStr, err)
		}
		minute, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid minute in %q: %w", timeStr, err)
		}
	} else {
		hour, err = strconv.Atoi(timeStr)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid hour in %q: %w", timeStr, err)
		}
		minute = 0
	}

	// Validate ranges
	if hour < 1 || hour > 12 {
		return 0, 0, fmt.Errorf("hour %d out of range 1-12", hour)
	}
	if minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("minute %d out of range 0-59", minute)
	}

	// Convert to 24-hour format
	if isPM && hour != 12 {
		hour += 12
	} else if isAM && hour == 12 {
		hour = 0
	}

	return hour, minute, nil
}

// FormatWaitMessage creates a human-readable message about the rate limit wait.
func FormatWaitMessage(stdout, stderr string) string {
	combined := stdout + "\n" + stderr

	matches := rateLimitPattern.FindStringSubmatch(combined)
	if matches == nil {
		return fmt.Sprintf("Rate limit hit, waiting %v to resume...", DefaultRateLimitWait)
	}

	timeStr := matches[1]
	tzStr := matches[2]

	// Parse to get the actual resume time with buffer
	resetTime, err := parseResetTime(timeStr, tzStr)
	if err != nil {
		return fmt.Sprintf("Rate limit hit, waiting %v to resume (time parse error: %v)", DefaultRateLimitWait, err)
	}

	// Add buffer to display the actual resume time
	resumeTime := resetTime.Add(RateLimitBuffer)

	// Format the resume time in 12-hour format
	resumeTimeStr := resumeTime.Format("3:04pm")

	// When the message omitted a timezone, the reset time was interpreted in
	// system local time; label it with the local zone abbreviation.
	tzLabel := tzStr
	if tzLabel == "" {
		tzLabel = resumeTime.Format("MST")
	}

	return fmt.Sprintf("Rate limit hit, waiting until %s %s to resume...", resumeTimeStr, tzLabel)
}

// spendLimitPattern matches the Claude org monthly spend-limit message. Unlike
// rolling usage limits, this message carries no reset time - no API exposes one
// either - so the loop cannot compute a wait duration and must probe until the
// limit lifts.
var spendLimitPattern = regexp.MustCompile(`You've hit your org's monthly spend limit`)

// ProbeInterval is how long the loop waits between probe attempts while the org
// monthly spend limit is in effect.
const ProbeInterval = 30 * time.Minute

// spendLimitProbePrompt is the trivial prompt used to check whether the spend
// limit has lifted. It is deliberately minimal to keep probe cost negligible.
const spendLimitProbePrompt = "ping"

// DetectSpendLimit reports whether the output indicates the org monthly spend
// limit has been reached. It checks both stdout and stderr, as the message may
// appear in either.
func DetectSpendLimit(stdout, stderr string) bool {
	combined := stdout + "\n" + stderr
	return spendLimitPattern.MatchString(combined)
}

// SpendLimitNoticeMessage explains that the org monthly spend limit was hit,
// that no reset time is available, and that the loop will probe until recovery.
func SpendLimitNoticeMessage() string {
	return fmt.Sprintf("Org monthly spend limit reached. No reset time is available for this limit, "+
		"so the loop will probe with a minimal Claude invocation every %v until usage is restored.", ProbeInterval)
}

// limitOutcome describes how handleClaudeLimits classified a Claude invocation.
type limitOutcome int

const (
	// limitNone means no usage limit was detected; the caller handles the
	// invocation result normally.
	limitNone limitOutcome = iota
	// limitWaited means a usage limit was detected and has since cleared; the
	// caller should retry the iteration without counting it against max
	// iterations.
	limitWaited
)

// handleClaudeLimits inspects a Claude invocation result for the two kinds of
// usage limit and blocks until the limit clears.
//
// Rolling usage limits carry a reset time, so the loop sleeps until that time
// (plus a buffer) before retrying. The org monthly spend limit carries no reset
// time, so the loop enters probe mode and periodically retries a minimal Claude
// invocation until one succeeds.
//
// It returns limitWaited if a limit was detected and has since cleared (the
// caller should retry the iteration without counting it) or limitNone if none
// was present. A non-nil error is returned only when ctx is cancelled while
// waiting or probing (Ctrl+C / session timeout), letting the caller take the
// existing graceful shutdown path.
//
// auth and projectDir exist so the spend-limit probe authenticates with the
// credential the run resolved. A probe on the wrong account answers the wrong
// question: it would report the limit lifted while the account actually doing
// the work is still capped, and the loop would spin on a limit that never clears.
func handleClaudeLimits(ctx context.Context, result *internal.PromptResult, auth domain.AuthMode, projectDir string) (limitOutcome, error) {
	if result == nil {
		return limitNone, nil
	}

	// Rolling usage limit: wait until the reported reset time.
	if DetectRateLimit(result.Stdout, result.Stderr) {
		waitDuration, parseErr := ParseRateLimitWait(result.Stdout, result.Stderr)
		if parseErr != nil {
			fmt.Printf("  Rate limit detected but failed to parse reset time: %v\n", parseErr)
			fmt.Printf("  Falling back to %v wait...\n", DefaultRateLimitWait)
		}
		fmt.Printf("  %s\n", FormatWaitMessage(result.Stdout, result.Stderr))

		select {
		case <-ctx.Done():
			return limitWaited, ctx.Err()
		case <-time.After(waitDuration):
			return limitWaited, nil
		}
	}

	// Org monthly spend limit: no reset time exists, so probe until it lifts.
	if DetectSpendLimit(result.Stdout, result.Stderr) {
		probeCLI := internal.NewCLI().WithAuth(auth, filepath.Join(projectDir, ".utopia"))
		probe := func(pctx context.Context) (*internal.PromptResult, error) {
			return probeCLI.Prompt(pctx, spendLimitProbePrompt)
		}
		if err := probeUntilRecovered(ctx, probe, ProbeInterval); err != nil {
			return limitWaited, err
		}
		return limitWaited, nil
	}

	return limitNone, nil
}

// probeUntilRecovered enters spend-limit probe mode: it periodically runs a
// minimal Claude invocation until the spend limit lifts. Each attempt is logged
// with a timestamp and its outcome (still limited or recovered). It returns nil
// once a probe succeeds, or a context error if ctx is cancelled during a wait
// or probe (Ctrl+C / session timeout), so the caller can take the graceful
// shutdown path. Probe attempts never count against the max iterations limit.
func probeUntilRecovered(ctx context.Context, probe func(context.Context) (*internal.PromptResult, error), interval time.Duration) error {
	fmt.Printf("  %s\n", SpendLimitNoticeMessage())

	for {
		// Wait out the probe interval. Ctrl+C or the elapsing session timeout
		// cancels ctx and drops us onto the graceful shutdown path.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		ts := time.Now().Format("2006-01-02 15:04:05")
		result, err := probe(ctx)

		// If the wait or the probe itself was cancelled, shut down gracefully
		// rather than interpreting the aborted probe as a recovery.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		stillLimited := result != nil && DetectSpendLimit(result.Stdout, result.Stderr)
		switch {
		case !stillLimited && err == nil:
			fmt.Printf("  [%s] probe succeeded: org monthly spend limit lifted, resuming...\n", ts)
			return nil
		case stillLimited:
			fmt.Printf("  [%s] probe: still limited\n", ts)
		default:
			// Probe failed for a reason other than the spend limit (e.g. a
			// transient error). Treat as still limited and keep probing.
			fmt.Printf("  [%s] probe: still limited (probe error: %v)\n", ts, err)
		}
	}
}
