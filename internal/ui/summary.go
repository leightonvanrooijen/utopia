package ui

import (
	"fmt"
	"sort"
	"strings"
)

// SummaryItem is one draft entry in a discovery summary.
type SummaryItem struct {
	Confidence    string // "high", "medium", or "low"
	Title         string
	Details       []string // pre-formatted "Label: value" lines
	Uncertainties []string
}

// Summary describes a "COMPLETE" report for a discovery run; the caller
// supplies the wording and per-item detail lines, the renderer owns layout.
type Summary struct {
	BannerTitle  string // e.g. "DISCOVERY COMPLETE"
	CreatedNoun  string // e.g. "draft specifications"
	SectionTitle string // e.g. "Draft Specifications:"
	DraftsDir    string
	Items        []SummaryItem
	NextSteps    []string
}

var confidenceOrder = map[string]int{"high": 0, "medium": 1, "low": 2}

// Summary renders the banner, confidence counts, per-draft details, and next
// steps for a completed discovery run, sorting items high → low confidence.
func (p *Printer) Summary(s Summary) {
	sort.Slice(s.Items, func(i, j int) bool {
		return confidenceOrder[s.Items[i].Confidence] < confidenceOrder[s.Items[j].Confidence]
	})
	counts := map[string]int{}
	for _, item := range s.Items {
		counts[item.Confidence]++
	}

	p.Banner(s.BannerTitle)
	fmt.Fprintf(p.out, "Created %d %s:\n", len(s.Items), s.CreatedNoun)
	fmt.Fprintf(p.out, "  %s HIGH confidence:   %d\n", Bullet, counts["high"])
	fmt.Fprintf(p.out, "  %s MEDIUM confidence: %d\n", Bullet, counts["medium"])
	fmt.Fprintf(p.out, "  %s LOW confidence:    %d\n", Bullet, counts["low"])
	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "Drafts saved to:", s.DraftsDir)
	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, s.SectionTitle)
	p.Rule()
	for _, item := range s.Items {
		fmt.Fprintf(p.out, "\n%s [%s] %s\n", ConfidenceGlyph(item.Confidence), strings.ToUpper(item.Confidence), item.Title)
		for _, detail := range item.Details {
			fmt.Fprintf(p.out, "  %s\n", detail)
		}
		if len(item.Uncertainties) > 0 {
			fmt.Fprintln(p.out, "  Uncertainties:")
			for _, note := range item.Uncertainties {
				fmt.Fprintf(p.out, "    %s %s\n", Warning, note)
			}
		}
	}
	fmt.Fprintln(p.out)
	p.Rule()
	fmt.Fprintln(p.out, "Next steps:")
	for _, step := range s.NextSteps {
		fmt.Fprintf(p.out, "  %s\n", step)
	}
	fmt.Fprintln(p.out)
}
