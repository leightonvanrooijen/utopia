# Declared executor tier - "this item is opus off the bat"

Parked 2026-07-30. Waiting on the chunking-agent CR to land, because that CR
decides whether chunk has an LLM in it, and phase 2 below depends on the answer.

## The gap

An architectural work item is expensive to get wrong and cheap to get right once
- exactly inverted from the escalation premise, which assumes the cheap tier is
the right first guess. Paying sonnet to fail twice on a boundary decision costs
more than opus getting it first pass.

Today tier is a pure function of failure history. `internal/ralph/escalation.go:395`:

```go
func executorModelFor(item *domain.WorkItem, defaultModel, escalatedModel string) string {
	if item.ComprehensionCount > 0 {
		return escalatedModel
	}
	return defaultModel
}
```

Nothing about the item's own nature enters it. Only lever today is
`utopia execute <cr-id> --model opus`, which is whole-invocation - so the
workaround is splitting architecture into its own CR.

`estimated_complexity: low|medium|high` already exists on every work item
(`workitem.go:51`, defaulted to medium at `workitem.go:193`) and NOTHING reads
it. Dead field as far as routing goes. Rejected as the signal anyway: it
conflates "big" with "needs judgement", and a high-complexity mechanical item
would burn opus for nothing.

## Decisions taken

1. **Field on the work item**: `executor_tier: default|escalated`. Not derived
   from complexity, not a config key.

2. **Floor, not pin.** A declared-escalated item must still be able to escalate
   ABOVE its floor (`fable` in utopia's own config) and still route to scoping.
   Otherwise declaring opus removes the item's escape hatch.

   ```go
   if item.ComprehensionCount > 0 { return escalatedModel }
   if item.ExecutorTier == TierEscalated { return escalatedModel }
   return defaultModel
   ```

3. **Do not charge the declared tier against `opus_execution_attempts`.**
   `chargeEscalatedAttempt` (`escalation.go:414`) books whenever
   `ComprehensionCount > 0`; that guard stays exactly as is. A declared item with
   zero comprehension failures is not escalated and gets its normal
   `verification.max_iterations` budget. Charge it and you halt it as
   `needs_human` after 2 clean-but-failing attempts. That cap bounds the
   ESCALATION PATH, not opus usage - the distinction needs saying out loud in the
   spec or someone will "fix" it.

4. **Declared in the change request, chunker copies it.** Feature-level
   `executor_tier` on the CR, copied onto the work item at `chunk.go:80` and
   `chunk.go:136` the same way `hints` and `constraints` already travel. Chunk
   stays deterministic and free.

   Rejected: adding an LLM judgement pass to chunk. It costs chunk its
   determinism and its zero token cost. REVISIT once the chunking-agent CR lands
   - if chunk gains an LLM anyway, inference becomes nearly free and this
   decision should be re-argued.

## Facts found while scoping (verify before trusting - as of 2026-07-30)

- `utopia chunk` has NO LLM. `grep 'NewCLI|claude|Prompt(ctx'` over
  `internal/chunk/` and `internal/cli/chunk.go` returns zero hits. `ChunkCR`
  (`chunk.go:78-100`) loops features, one work item each, `Order = i`.
- **No load-time validation of work-item enums exists.** `status` and
  `estimated_complexity` are bare string constants (`workitem.go:13-27`); nothing
  validates them on read. So "unknown executor_tier fails at load" means BUILDING
  the first such validation, not extending one.
- `Feature.Hints` is the ephemerality precedent: parsed from CR YAML, stripped
  before saving to the spec at `spec.go:111,124`. Open question - should
  `executor_tier` be stripped the same way? It describes HOW to build, not what
  exists, so probably yes, same category as hints.
- `RoutingRecord` is `internal/domain/documentation.go:196-241` (YAML tags,
  not json), written via `internal/store.go:774` to
  `.utopia/runs/{cr_id}/{workitem_id}.yaml`.
- `SonnetAttempts` / `OpusExecutionAttempts` on the record are named for the
  roles' usual models. Declared-tier attempts would pollute
  `OpusExecutionAttempts` and therefore the escalation rate per spec, which is
  the specific number `escalation-telemetry` exists to make arguable. Needs
  either a separate counter or an explicit exclusion.
- `ralph.go:430` prints one message for both escalation paths
  (`invoking Claude on the escalated executor`). A declared item would print it
  on iteration 1 having failed nothing, which reads as a bug. Needs to
  distinguish declared from escalated-into.

## Intended CR shape (not written)

`initiative`, 2 phases:

- **Phase 1** - `escalation-routing`, new feature `declared-executor-tier`: the
  field, the floor semantics, the cap exclusion, load-time validation, and the
  log line distinguishing declared from escalated-into.
- **Phase 2** - `unified-change-request-system` + `chunking-ralph-sequential`:
  feature-level `executor_tier` on the CR schema, `cr validate` accepting it,
  chunker propagation, and the telemetry split so escalation rate is not
  polluted by work that never escalated.

## Related, already applied

Greenfield (`~/Ideaprojects/greenfield/.utopia/config.yaml`) now has the tiered
setup: sonnet execute / opus execute_escalated + scoper / haiku validator_router,
efforts, and the escalation caps documented-but-commented at their defaults.

Outstanding there: both its validators (`verification-gate.md`,
`purged-name-guard.md`) still emit the legacy bare `<PASSED>`. A failure with no
`<VERDICT>` block is read as COMPREHENSION (`validators/verdict.go` rule 4), so
every rejection jumps straight to opus and the cheap mechanical-retry path is
never taken. `purged-name-guard` is the easy fix - deterministic `git grep`, so
its failures are almost always mechanical.
