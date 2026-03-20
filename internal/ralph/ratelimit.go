package ralph

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rateLimitPattern matches the Claude rate limit message format:
// "Limit reached · resets {time} ({timezone})"
// Examples:
// - "Limit reached · resets 1am (Australia/Sydney)"
// - "Limit reached · resets 12:30pm (America/New_York)"
var rateLimitPattern = regexp.MustCompile(`Limit reached.*resets\s+(\d{1,2}(?::\d{2})?(?:am|pm))\s+\(([^)]+)\)`)

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
func parseResetTime(timeStr, tzStr string) (time.Time, error) {
	// Load the timezone
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", tzStr, err)
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

	return fmt.Sprintf("Rate limit hit, waiting until %s %s to resume...", resumeTimeStr, tzStr)
}
