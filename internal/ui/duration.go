package ui

import (
	"fmt"
	"time"
)

// Duration renders d for a human reading a log line: "1.2s" below a minute,
// "3m04s" below an hour, "1h03m04s" beyond. Minutes and seconds are
// zero-padded so successive lines align in a scrolling log, and sub-minute
// durations keep one decimal so a fast step reads as "0.4s" rather than "0s".
//
// Go's own Duration.String() is not used because it renders a long run as
// "3m4.216482931s" - precision no operator needs, at a width that makes
// columns of timings unreadable.
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	// Round before the threshold test so 59.97s reports as "1m00s" rather
	// than the impossible "60.0s".
	d = d.Round(100 * time.Millisecond)
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	total := int(d.Round(time.Second).Seconds())
	hours, minutes, seconds := total/3600, (total%3600)/60, total%60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}
