package ralph

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestStepTimings_SummaryReportsTotalAndPerCategoryBreakdown(t *testing.T) {
	timings := &stepTimings{
		start:        time.Now().Add(-(14*time.Minute + 22*time.Second)),
		claude:       11*time.Minute + 30*time.Second,
		verification: 2*time.Minute + 10*time.Second,
		validators:   2100 * time.Millisecond,
	}

	got := timings.summary()

	for _, want := range []string{"total 14m22s", "claude 11m30s", "verification 2m10s", "validators 2.1s"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary must report %q, got %q", want, got)
		}
	}
}

func TestStepTimings_SummaryReportsUntimedStepsAsZero(t *testing.T) {
	// A work item that completed with no verification command and no
	// validators configured still reports every category, so the breakdown
	// has a fixed shape across runs.
	got := newStepTimings().summary()

	for _, want := range []string{"verification 0.0s", "validators 0.0s"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary must report %q, got %q", want, got)
		}
	}
}

// The resolution ledger is where a validator or connector run reports how long
// it actually took, labelled with its name - the loop only sees the aggregate
// outcome at the join.
func TestResolutionLedger_RecordsRunDurationLabelledWithName(t *testing.T) {
	sub := Subscription{
		Name:   "validators:after-workitem",
		Launch: EventWorkItemVerified,
		Join:   EventWorkItemVerified,
		Action: commandAction(domain.ConnectorConfig{Name: "validators:after-workitem", Command: "sleep 0.2"}, t.TempDir()),
	}
	en := NewEngine([]Subscription{sub})

	out := captureStdout(t, func() {
		if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
			t.Fatalf("emit failed: %v", err)
		}
	})

	// A duration, not a fixed value: the assertion is that the elapsed run
	// time is reported next to the name, and that it is plausibly the 200ms
	// the action slept for rather than a zeroed clock.
	want := regexp.MustCompile(`validators:after-workitem ` + handleJoined + ` in 0\.[2-9]s`)
	if !want.MatchString(out) {
		t.Errorf("ledger must report the run duration beside the name, got:\n%s", out)
	}
}
