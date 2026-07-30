package domain

// CostBasis says what a recorded dollar figure means, because the same number
// means two different things depending on which credential the attempt ran with.
//
// Tokens are a fact under both auth modes: the model read and wrote them however
// the run was paid for. Dollars are not. Under AuthModeAPIKey the claude CLI's
// total_cost_usd is money that will appear on an invoice. Under
// AuthModeSubscription no per-token charge is incurred at all, so the same field
// is a list-price equivalent - what those tokens would have cost had they been
// billed - and summing it with charged dollars produces a figure that is neither.
// See ADR-007.
type CostBasis string

const (
	// CostBasisCharged marks a cost produced under api-key auth: an amount that
	// will be billed.
	CostBasisCharged CostBasis = "charged"
	// CostBasisListPriceEstimate marks a cost produced under subscription auth: a
	// list-price equivalent of the tokens spent, not an amount charged.
	CostBasisListPriceEstimate CostBasis = "list-price-estimate"
	// CostBasisUnknown marks a cost whose auth mode was not resolved by Utopia,
	// which is the ambient-environment case - the invocation inherited whatever
	// credential the environment held, so which of the two above applies is not
	// knowable from the record.
	CostBasisUnknown CostBasis = "unknown"
)

// CostBasisForAuth maps the auth mode an invocation ran under to what its
// reported dollar figure means.
func CostBasisForAuth(mode AuthMode) CostBasis {
	switch mode {
	case AuthModeAPIKey:
		return CostBasisCharged
	case AuthModeSubscription:
		return CostBasisListPriceEstimate
	default:
		return CostBasisUnknown
	}
}

// AttemptUsage is what one Claude invocation spent, as the claude CLI reported it
// on the terminal result object of a structured-output run.
//
// Available is what separates the three states a reader has to tell apart, and it
// is why this type is carried by pointer wherever it is recorded:
//
//   - a nil *AttemptUsage: usage was never captured for this attempt, either
//     because the invocation predates the capture or because it was made by a
//     role outside the execution loop (a validator, discovery)
//   - Available false: usage was asked for and could not be read, so the spend
//     happened and is not known
//   - Available true with zero counts: the CLI reported those counts as zero
//
// Neither of the first two may be folded into a total as zero spend, which is why
// the flag is serialised unconditionally rather than left to omitempty.
type AttemptUsage struct {
	// Available reports whether the counts below were actually read off a result
	// object. False means they are absent, not zero.
	Available bool `yaml:"available"`

	// UnavailableReason says why the usage could not be read, for a reader looking
	// at a gap in the record. Empty when Available is true.
	UnavailableReason string `yaml:"unavailable_reason,omitempty"`

	// Model is the model id the claude CLI resolved to, not the alias passed to
	// --model. A comparison keyed on "opus" would silently pool every model that
	// alias has ever pointed at, so the record keeps what actually ran.
	Model string `yaml:"model,omitempty"`

	// Effort is the reasoning effort the invocation ran at, empty when none was
	// configured and the CLI's own default applied.
	Effort string `yaml:"effort,omitempty"`

	// Token counts as the result object reported them.
	InputTokens         int `yaml:"input_tokens,omitempty"`
	OutputTokens        int `yaml:"output_tokens,omitempty"`
	CacheReadTokens     int `yaml:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `yaml:"cache_creation_tokens,omitempty"`

	// Turns is how many turns the invocation spent.
	Turns int `yaml:"turns,omitempty"`

	// DurationMS is the invocation's wall-clock duration in milliseconds.
	DurationMS int64 `yaml:"duration_ms,omitempty"`

	// CostUSD is the dollar figure the CLI reported, and CostBasis is what that
	// figure means. The two are recorded together deliberately: a cost read without
	// its basis is not interpretable.
	CostUSD   float64   `yaml:"cost_usd,omitempty"`
	CostBasis CostBasis `yaml:"cost_basis,omitempty"`
}

// UnavailableUsage records that an attempt ran and its usage could not be read.
// It is not an error condition of the work: the attempt's code changes stand
// whether or not the accounting came back.
func UnavailableUsage(reason string) *AttemptUsage {
	return &AttemptUsage{Available: false, UnavailableReason: reason}
}

// IsAvailable reports whether this usage carries counts a total may include. It
// is nil-safe so a caller summing over records that predate capture, or over
// attempts made outside the execution loop, does not have to special-case them.
func (u *AttemptUsage) IsAvailable() bool {
	return u != nil && u.Available
}

// CostIsCharged reports whether the recorded cost is money billed rather than a
// list-price equivalent. A caller summing dollars uses it to keep the two apart.
func (u *AttemptUsage) CostIsCharged() bool {
	return u.IsAvailable() && u.CostBasis == CostBasisCharged
}

// AttemptOutcome is what one execution attempt achieved. It is a separate
// vocabulary from RunOutcome and RoutingOutcome, which describe a whole run: an
// attempt is one Claude invocation, and most attempts of a failed run are not
// themselves the reason the run failed.
type AttemptOutcome string

const (
	// AttemptPassed means the attempt's work cleared verification and every gate,
	// which is the attempt that completed the work item.
	AttemptPassed AttemptOutcome = "passed"
	// AttemptFailed means the attempt produced work that was rejected - verification
	// failed, or a validation gate blocked it. The rejecting class rides alongside as
	// FailureClass.
	AttemptFailed AttemptOutcome = "failed"
	// AttemptIncomplete means the attempt never claimed to be done, so nothing it
	// produced was judged: it spent its turn budget, or it stopped without the
	// completion token.
	AttemptIncomplete AttemptOutcome = "incomplete"
	// AttemptErrored means the invocation itself failed, so the attempt has no
	// verdict and no claim. Distinct from AttemptIncomplete, where the agent ran and
	// simply did not finish.
	AttemptErrored AttemptOutcome = "errored"
)

// UsageEntry is one iteration's line in a run record's usage list: which model at
// which effort ran that iteration, what it achieved, and what it spent.
//
// The spend is inlined rather than nested so an entry is one flat row - the shape
// a comparison of models and efforts reads - and so Available stays on the entry
// itself, where a reader summing a column cannot miss it.
type UsageEntry struct {
	// Iteration is the work item's iteration number this entry accounts for. Entries
	// are ordered by it, one per iteration that actually ran.
	Iteration int `yaml:"iteration"`
	// Role is ExecutorRoleDefault or ExecutorRoleEscalated, so a per-tier total does
	// not have to be inferred from the model.
	Role string `yaml:"role,omitempty"`
	// Outcome is what the attempt achieved, and FailureClass is why it was rejected
	// when it was.
	Outcome      AttemptOutcome `yaml:"outcome,omitempty"`
	FailureClass string         `yaml:"failure_class,omitempty"`

	// AttemptUsage carries the resolved model, the effort, the token counts and the
	// cost with its basis. Model is the id the claude CLI resolved, falling back to
	// the model routing asked for when the CLI reported none.
	AttemptUsage `yaml:",inline"`
}

// UsageEntriesFor projects a work item's execution attempts onto the run record's
// usage list, in attempt order.
//
// An attempt whose usage was never captured becomes an entry marked unavailable
// rather than being dropped: the iteration ran and spent tokens, so a list that
// omitted it would read as a run with fewer iterations than it had. Nothing here
// invents a count.
func UsageEntriesFor(attempts []ExecutorAttempt) []UsageEntry {
	if len(attempts) == 0 {
		return nil
	}
	entries := make([]UsageEntry, 0, len(attempts))
	for _, a := range attempts {
		usage := AttemptUsage{Available: false, UnavailableReason: "no usage was captured for this attempt"}
		if a.Usage != nil {
			usage = *a.Usage
		}
		// The model routing asked for is better than nothing when the CLI reported no
		// resolved id, and it is marked as what it is by the entry's Available flag.
		if usage.Model == "" {
			usage.Model = a.Model
		}
		if usage.Effort == "" {
			usage.Effort = a.Effort
		}
		entries = append(entries, UsageEntry{
			Iteration:    a.Iteration,
			Role:         a.Role,
			Outcome:      a.Outcome,
			FailureClass: a.FailureClass,
			AttemptUsage: usage,
		})
	}
	return entries
}

// UsageTotals is what a set of usage entries spent, summed. It exists so a total
// for a work item or a change request is a read of the persisted entries rather
// than a re-read of the transcripts.
//
// Dollars are kept in three columns because charged money and a list-price
// equivalent of subscription tokens are different quantities (see CostBasis), and
// one figure summing both is neither.
type UsageTotals struct {
	// Entries is how many iterations were summed, Available how many of them carried
	// counts, and Unavailable how many ran with their accounting unreadable. A
	// non-zero Unavailable is why the token and dollar figures below are a floor
	// rather than the whole spend.
	Entries     int
	Available   int
	Unavailable int

	// RecordsWithoutUsage counts run records that carry no usage list at all - runs
	// written before usage was persisted. They are unknown spend, not zero, and are
	// reported separately so a caller can say so rather than publishing a total that
	// silently excludes them.
	RecordsWithoutUsage int

	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	Turns               int

	ChargedCostUSD      float64
	ListPriceCostUSD    float64
	UnknownBasisCostUSD float64
}

// TotalTokens is every token column summed - what the models read and wrote,
// cache included.
func (t UsageTotals) TotalTokens() int {
	return t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheCreationTokens
}

// Complete reports whether every entry summed carried counts, and every record
// summed carried a usage list. False means the totals are a floor.
func (t UsageTotals) Complete() bool {
	return t.Unavailable == 0 && t.RecordsWithoutUsage == 0
}

// Add sums two totals, so a per-change-request total is the per-work-item totals
// folded together.
func (t UsageTotals) Add(o UsageTotals) UsageTotals {
	t.Entries += o.Entries
	t.Available += o.Available
	t.Unavailable += o.Unavailable
	t.RecordsWithoutUsage += o.RecordsWithoutUsage
	t.InputTokens += o.InputTokens
	t.OutputTokens += o.OutputTokens
	t.CacheReadTokens += o.CacheReadTokens
	t.CacheCreationTokens += o.CacheCreationTokens
	t.Turns += o.Turns
	t.ChargedCostUSD += o.ChargedCostUSD
	t.ListPriceCostUSD += o.ListPriceCostUSD
	t.UnknownBasisCostUSD += o.UnknownBasisCostUSD
	return t
}

// TotalUsage sums usage entries. Entries whose usage was unavailable are counted
// and contribute nothing: their spend happened and is not known, and folding it
// in as zero would understate every figure here.
func TotalUsage(entries []UsageEntry) UsageTotals {
	var t UsageTotals
	for _, e := range entries {
		t.Entries++
		if !e.Available {
			t.Unavailable++
			continue
		}
		t.Available++
		t.InputTokens += e.InputTokens
		t.OutputTokens += e.OutputTokens
		t.CacheReadTokens += e.CacheReadTokens
		t.CacheCreationTokens += e.CacheCreationTokens
		t.Turns += e.Turns
		switch e.CostBasis {
		case CostBasisCharged:
			t.ChargedCostUSD += e.CostUSD
		case CostBasisListPriceEstimate:
			t.ListPriceCostUSD += e.CostUSD
		default:
			t.UnknownBasisCostUSD += e.CostUSD
		}
	}
	return t
}

// UsageTotals is the work item's total spend, read off its own run record.
//
// A record with no usage list is unknown spend rather than zero: it reports one
// RecordsWithoutUsage and no counts, so a caller cannot mistake a run written
// before usage was persisted for a run that spent nothing.
func (r *ExecutionRun) UsageTotals() UsageTotals {
	if len(r.Usage) == 0 {
		return UsageTotals{RecordsWithoutUsage: 1}
	}
	return TotalUsage(r.Usage)
}

// TotalRunUsage is the change request's total spend: every work item's run record
// in its run directory, folded together. Records carrying no usage list are
// counted in RecordsWithoutUsage, so an incomplete total says that it is one.
func TotalRunUsage(runs []*ExecutionRun) UsageTotals {
	var t UsageTotals
	for _, run := range runs {
		if run == nil {
			continue
		}
		t = t.Add(run.UsageTotals())
	}
	return t
}
