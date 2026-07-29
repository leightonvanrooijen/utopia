package cli

import (
	"sort"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// crFile builds an ordering fixture. basename is the on-disk filename (".yaml"
// stripped) that drives ordering; id is the CR's internal id, which must not.
func crFile(basename, id string) internal.ChangeRequestFile {
	return internal.ChangeRequestFile{
		Basename: basename,
		CR:       &domain.ChangeRequest{ID: id},
	}
}

func TestLessCRExecutionOrder(t *testing.T) {
	// Input is deliberately scrambled; sorting must produce the sequence below.
	// Filename and internal id agree here, which is the simple case.
	crs := []internal.ChangeRequestFile{
		crFile("zebra", "zebra"),
		crFile("10_tenth", "10_tenth"),
		crFile("2_second", "2_second"),
		crFile("apple", "apple"),
		crFile("1_first", "1_first"),
		crFile("02_also-second", "02_also-second"),
	}

	sortCRFiles(crs)

	// Numeric prefixes first, compared numerically (2 before 10, not "10" < "2");
	// equal sequence numbers tie-break alphabetically ("02_also" before "2_second").
	// Non-prefixed CRs come last, alphabetically.
	want := []string{"1_first", "02_also-second", "2_second", "10_tenth", "apple", "zebra"}
	assertOrder(t, crs, want)
}

// TestLessCRExecutionOrder_FilenameNotID is the regression test for the ordering
// bug: the sort read cr.ID, which under the project's convention never carries
// the prefix, so every CR looked unprefixed and the batch silently ran in
// alphabetical id order. Every fixture's internal id here is deliberately
// anti-correlated with its filename position, so an id-based sort produces the
// exact reverse of the expected order.
func TestLessCRExecutionOrder_FilenameNotID(t *testing.T) {
	crs := []internal.ChangeRequestFile{
		crFile("03_third", "aaa-runs-last-if-id-wins"),
		crFile("01_first", "zzz-runs-first-if-id-wins"),
		crFile("02_second", "mmm-middle"),
	}

	sortCRFiles(crs)

	// Filenames decide: 01_, 02_, 03_. Sorting on the ids would give
	// aaa, mmm, zzz - i.e. 03_, 02_, 01_.
	want := []string{"01_first", "02_second", "03_third"}
	assertOrder(t, crs, want)
}

// TestLessCRExecutionOrder_PrefixedFilenameUnprefixedID covers the real-data
// shape the convention produces: 01_reusable-core.yaml containing
// id: reusable-core. The clean id has no prefix to find, so an id-based sort
// treats the CR as unprefixed and drops it into the alphabetical tail - behind
// genuinely unprefixed CRs. Ordering by filename keeps it ahead of them.
func TestLessCRExecutionOrder_PrefixedFilenameUnprefixedID(t *testing.T) {
	prefixed := crFile("01_reusable-core", "reusable-core")
	unprefixed := crFile("cleanup-legacy", "cleanup-legacy")

	if !lessCRExecutionOrder(prefixed, unprefixed) {
		t.Errorf("lessCRExecutionOrder(%q, %q) = false, want true: a prefixed filename must sort ahead of an unprefixed one even when its internal id carries no prefix",
			prefixed.Basename, unprefixed.Basename)
	}
	if lessCRExecutionOrder(unprefixed, prefixed) {
		t.Errorf("lessCRExecutionOrder(%q, %q) = true, want false: the ordering must be asymmetric",
			unprefixed.Basename, prefixed.Basename)
	}

	// And the same rule holds through a real sort, with the unprefixed CR's id
	// sorting alphabetically ahead of the prefixed CR's id ("cleanup" < "reusable").
	crs := []internal.ChangeRequestFile{unprefixed, prefixed}
	sortCRFiles(crs)
	assertOrder(t, crs, []string{"01_reusable-core", "cleanup-legacy"})
}

func sortCRFiles(crs []internal.ChangeRequestFile) {
	sort.Slice(crs, func(i, j int) bool { return lessCRExecutionOrder(crs[i], crs[j]) })
}

func assertOrder(t *testing.T, crs []internal.ChangeRequestFile, want []string) {
	t.Helper()
	if len(crs) != len(want) {
		t.Fatalf("got %d CRs, want %d", len(crs), len(want))
	}
	for i, cr := range crs {
		if cr.Basename != want[i] {
			t.Errorf("position %d = %q, want %q (full order: %v)", i, cr.Basename, want[i], basenames(crs))
		}
	}
}

func basenames(crs []internal.ChangeRequestFile) []string {
	out := make([]string, len(crs))
	for i, cr := range crs {
		out[i] = cr.Basename
	}
	return out
}
