# Cache control, batch submission and token accounting need an API client

Cut from CR `12_tiered-model-routing-validation-escalation` during authoring.
Recorded here rather than dropped, because the reasoning is not obvious from
the outside and the next person to design in this space will assume otherwise.

## The boundary

Utopia does not call the Anthropic API. It shells out to the `claude` CLI -
`internal/claude.go:110-134`, `claude --print --verbose --model <alias>` - and
parses plain-text stdout for sentinel tokens (`<COMPLETE>`, `<PASSED>`).

That boundary is fine for anything expressible as a flag. `--model` and
`--effort` both are, which is why tiered routing and the effort policy survived
into CR 12. It is not fine for anything that requires shaping a request body or
reading a response envelope.

## What is out of reach, and why

**Prompt cache control.** There is no `cache_control` to set, no breakpoint to
place, and no control over prompt segment ordering - the CLI assembles its own
prompt. Claude Code does cache internally, so the benefit is partly had for
free; it is just not a design surface Utopia can reason about or optimise.

Worth recording separately: the original design proposed sharing a cache prefix
between the executor and the validator. That would not work even with full API
access, because **prompt caches are model-scoped**. An Opus validator and a
Sonnet executor cannot share an entry no matter how byte-identical the prefix.
The stable-prefix discipline is sound within a role and impossible across roles,
and the tiered design is precisely what puts the two roles on different models.

**Batch API.** No submission path. Would require an API client.

**Token counts and monetary cost.** `claude --print` reports neither. So
`input_tokens`, `cached_input_tokens`, `output_tokens` and `estimated_cost_usd`
are absent from CR 12's telemetry record, and cost-per-accepted-change is
approximated from attempt counts times model tier rather than measured.

## The one plausible path short of an API client

`claude --output-format stream-json` emits a result envelope that carries usage,
including cache token counts. That would make real cost telemetry reachable
without becoming an API client.

It was rejected for now because it rewrites output parsing wholesale, and three
things depend on the current text stream: `<COMPLETE>` detection in the ralph
loop, `<PASSED>` and the new `<VERDICT>` block in validators, and the real-time
streaming in `streamingPrompt`. It also overlaps `06_observability-unified-output`.

If cost telemetry becomes the priority, this is the change to make - and it
should be its own CR, sequenced after 06, not folded into an escalation design.

## Why this matters beyond the cut

The design CR 12 came from reasoned entirely at the API layer: cache
breakpoints, batch discounts, per-token pricing, tokeniser inflation factors.
All of it was locally correct and none of it was reachable, because Utopia sits
one layer up. The split turned out clean - everything about *judgement routing*
translated, everything about *token mechanics* did not.

That is the generalisable lesson, and CR 12 asks for it as an ADR: **Utopia
forwards decisions to the CLI rather than modelling the API.** Corollaries worth
holding onto - do not restate downstream tool knowledge in Utopia's own tables
(the retired-model-ID bug in CR 11 was exactly that mistake), and check what the
CLI permits before concluding a knob does not exist (the `--effort` flag existed
the whole time; Utopia simply never passed it).

## Also noticed while reading

One correction to the source design's cost model, for whoever picks up the
estimator: Sonnet 5 and Opus 5 use the **same tokeniser**, so no inflation
factor is needed between those two. The ~1.0-1.35x figure applies versus the
Opus 4.x-and-earlier line. Sonnet-to-Opus comparisons are directly comparable,
which makes the break-even arithmetic simpler rather than harder.

Cache write cost is also worth having in any future estimator: reads are 0.1x
base input, but writes are 1.25x at the 5-minute TTL and 2x at the 1-hour TTL.
