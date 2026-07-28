package cli

import (
	"sort"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestCRNumericPrefix(t *testing.T) {
	tests := []struct {
		id       string
		wantN    int
		wantHave bool
	}{
		{"1_first", 1, true},
		{"2_second", 2, true},
		{"10_tenth", 10, true},
		{"02_padded", 2, true}, // zero-padding collapses to the same value
		{"000_zero", 0, true},  // an explicit zero prefix is still a prefix
		{"cleanup-legacy", 0, false},
		{"2024-migration", 0, false}, // digits not followed by "_" is not a prefix
		{"2a_mixed", 0, false},       // non-digit inside the run
		{"_leading", 0, false},       // empty digit run
		{"nodigits", 0, false},
	}
	for _, tt := range tests {
		gotN, gotHave := crNumericPrefix(tt.id)
		if gotN != tt.wantN || gotHave != tt.wantHave {
			t.Errorf("crNumericPrefix(%q) = (%d, %t), want (%d, %t)",
				tt.id, gotN, gotHave, tt.wantN, tt.wantHave)
		}
	}
}

func TestLessCRExecutionOrder(t *testing.T) {
	// Input is deliberately scrambled; sorting must produce the sequence below.
	crs := []*domain.ChangeRequest{
		{ID: "zebra"},
		{ID: "10_tenth"},
		{ID: "2_second"},
		{ID: "apple"},
		{ID: "1_first"},
		{ID: "02_also-second"},
	}

	sort.Slice(crs, func(i, j int) bool { return lessCRExecutionOrder(crs[i], crs[j]) })

	// Numeric prefixes first, compared numerically (2 before 10, not "10" < "2");
	// equal sequence numbers tie-break alphabetically ("02_also" before "2_second").
	// Non-prefixed CRs come last, alphabetically.
	want := []string{"1_first", "02_also-second", "2_second", "10_tenth", "apple", "zebra"}
	for i, cr := range crs {
		if cr.ID != want[i] {
			t.Errorf("position %d = %q, want %q (full order: %v)", i, cr.ID, want[i], ids(crs))
		}
	}
}

func ids(crs []*domain.ChangeRequest) []string {
	out := make([]string, len(crs))
	for i, cr := range crs {
		out[i] = cr.ID
	}
	return out
}
