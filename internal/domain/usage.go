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
