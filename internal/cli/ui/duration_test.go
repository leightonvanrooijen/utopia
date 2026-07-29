package ui

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub-second keeps a decimal", 420 * time.Millisecond, "0.4s"},
		{"seconds", 1234 * time.Millisecond, "1.2s"},
		{"just under a minute rounds up to minutes", 59970 * time.Millisecond, "1m00s"},
		{"exactly a minute", time.Minute, "1m00s"},
		{"minutes zero-pad seconds", 3*time.Minute + 4*time.Second, "3m04s"},
		{"hours zero-pad minutes and seconds", time.Hour + 3*time.Minute + 4*time.Second, "1h03m04s"},
		{"zero", 0, "0.0s"},
		{"negative clamps to zero", -time.Second, "0.0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Duration(tt.in); got != tt.want {
				t.Errorf("Duration(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
