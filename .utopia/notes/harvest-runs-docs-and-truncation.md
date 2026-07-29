# Harvest + execution runs: two loose ends

Found while investigating whether persisted Ralph runs auto-harvest (they did,
unconditionally). CR `09_harvest-runs-opt-in` makes that opt-in behind `--runs`,
defaulted off. These two items were deliberately left out of that CR.

## 1. README doesn't mention execution runs as a harvest source

- `README.md:118-122` describes harvest as scanning only "your conversations"
- The "Knowledge Artifacts" section (~`README.md:168`) says "from your conversations"

Before CR 09 this was straightforwardly stale - runs were on by default and
undocumented. After CR 09 the "conversations" wording becomes accurate again for
the default path, but `--runs` itself will be undocumented, along with:

- `.utopia/runs/` as a top-level directory users interact with
- that runs are system-truth sources feeding ADR + domain qualification
- that runs accumulate unprocessed until someone opts in

Mildly ironic: `readme-signal-detection` exists to catch exactly this, and a new
top-level `.utopia/` directory should qualify under its "new top-level .utopia/
directory users interact with" criterion. Worth checking why it didn't fire when
`run-transcript-persistence` merged - either the criteria aren't being applied to
newly-added directories, or the signal was surfaced and skipped.

## 2. Run transcripts truncate at 2000 chars per source

`writeTranscript` (`internal/harvest/harvest.go:867-878`) caps each embedded
transcript at 2000 characters.

A Ralph run accumulates *every* iteration into its transcript, prefixed
`--- iteration N ---`, including abandoned attempts. So for a work item that took
several iterations to converge, 2000 chars likely captures only the early failing
attempts and truncates away the iteration that actually succeeded - which is
precisely the part carrying the decision worth harvesting.

Not filed as a CR because it's a design question, not a defect. Options:

- Raise the cap (simple, costs tokens - runs against the grain of CR 09)
- Truncate from the *tail* rather than the head, keeping the final iteration
- Keep first + last iteration, drop the middle
- Summarize per-iteration at write time in the Ralph loop, so harvest reads
  something already compact

The tail-preserving variant looks cheapest and best-aligned with why runs are
worth harvesting at all. Note `execution-ralph` spec already has a
`domain_knowledge` invariant along the lines of "output truncation preserves
tail" - worth checking whether this truncation contradicts it.
