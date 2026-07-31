# Bug 1 from `/private/tmp/utopia-bugfix-handoff.md` is stale - do not re-derive

The handoff's headline item, "Acceptance criteria are silently dropped when the
sizer splits a feature", was verified against `main` on 2026-07-31. Its central
claim is no longer true. Recorded here so the next session does not spend the
context re-discovering it.

## What the handoff claimed

> `isPartitionOf` is correct, but there is **no `else`**. A split that loses
> criteria is labelled `authored` and execution proceeds.

## What the code actually does

`validateSplit` (`internal/chunk/sizer.go:323`) runs *before* anything reaches
`expandFeatures`. A split that is not a partition is rejected unless the feature
has exactly one criterion - the legal escalation case:

- `sizer.go:344-352` - partition, else single-criterion escalation, else error
- `sizer.go:286-292` - `applyAssessments` catches that error, keeps the feature
  whole, and records `split rejected: <reason>` on the verdict
- `internal/cli/execute.go:616-644` - `reportSizing` prints every verdict and its
  reason at chunk time

So by the time `expandFeatures` reaches `chunk.go:155`, a non-partition can only
be the single-criterion escalation. The missing `else` is correct by
construction, not a hole. Introduced by `786c4d9 feat: size features against the
turn budget before chunking`, which post-dates the handoff's analysis.

## What survives, and why it was not written as a bugfix

Two real gaps remain, but neither violates a written acceptance criterion, so
neither belongs in a `type: bugfix` CR:

**`CriteriaOrigin` is write-only.** Only writes exist - `chunk.go:155`, `:157`,
`:190`, plus the declarations in `internal/domain/workitem.go:21,27,32,79`. No
non-test code reads or branches on it. But the `work-item-sizing` criteria only
require that it be *recorded*, and the description's "visible rather than
silent" is now satisfied by `reportSizing` at chunk time. Nothing is broken.

**No cross-work-item criterion coverage.** Two places:

- `internal/ralph/ralph.go:258` (the handoff says 255) - `if result.Completed ==
  result.Total`, inside `Execute`. Counts work items that exited their loop.
  Nothing compares the union of satisfied criteria against the source feature's
  original list. `SourceFeatureID` is already the join key such a check would
  need. Owning feature would be `execution-ralph` / `sequential-executor`, whose
  criteria say nothing about coverage.
- `.utopia/validators/spec-intent.md:31-39` - the validator finds the single
  `in_progress` work item and reads criteria out of *that item's prompt*, which
  holds only its subset. Feature `[A,B,C,D]` split into `[A,B]` + `[C,D]` gets
  checked as `[A,B]` then `[C,D]`, never as `[A,B,C,D]`. The owning feature,
  `agentic-validators` / `default-spec-intent-validator`, does not constrain the
  discovery procedure at all - an ownership gap.

If this is picked up, it is an **enhancement**, adding criteria to
`sequential-executor` and `default-spec-intent-validator`. The validator half is
probably sufficient on its own; the `ralph.go` gate largely duplicates it.

## Unrelated defect noticed while verifying

Two `spec-intent.md` files exist and have drifted:

- `internal/cli/templates/spec-intent.md` - the embedded template init writes
  (`init.go:219`, `:307`). This is what `default-spec-intent-validator` governs.
- `.utopia/validators/spec-intent.md` - Utopia's own instance. Line 27 hardcodes
  `./scripts/verify.sh`, the exact thing the feature description forbids in the
  shipped prompt.

Because "Re-running `utopia init` leaves that file byte-for-byte unchanged", init
will never correct the local copy. The drift is permanent and invisible.

Also: that validator's Step 1.3 says to read the work item's `spec_ref` and open
the matching spec, but for CR-derived items `SpecRef` is the work item ID prefix
(`<crID>-phase-<n>`, `chunk.go:127`), not a spec ID. It will not resolve.

## Status of the other two handoff bugs

- **Bug 2** (seven discarded `SaveWorkItemForSpec` errors) - not written. CR 18,
  which collapses the seven sites into one helper, has already merged (commit
  `f2cea6d cleanup: complete 18_decompose-execute-work-item`), so the dependency
  the handoff flagged is now satisfied. Re-locate the sites before writing; they
  have moved.
- **Bug 3** (`renderTemplate` panics) - written as
  `.utopia/change-requests/22_render-template-panics-instead-of-returning-an-error.yaml`.
  Confirmed still present at `chunk.go:663` and `:669`. Owning feature is
  `prompt-builder`, not `prompt-template`: `prompt-template`'s criteria describe
  only the static `const PromptTemplate` literal, while `prompt-builder` owns the
  escaping that `renderTemplate` performs.

## Also stale in the handoff

It says CR 17 is the house-style reference and that 18-21 were written that
session. `17` and `18` are gone from `.utopia/change-requests/` - both merged and
cleaned up. Use `16_default-executor-ignores-configured-model-rewrite-1.yaml` as
the bugfix style reference instead.
