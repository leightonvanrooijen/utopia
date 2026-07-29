# Orphaned CR files from the shadow-save bug - RESOLVED (data), code fix still open

Data cleanup is DONE. The code fix is CR `fix-prefixed-cr-shadow-file-on-save`
and is still open - until it lands, this will recur in any repo using numeric
CR filename prefixes.

## What was cleaned (greenfield, ~/IdeaProjects/greenfield)

**1. `01_reusable-core.yaml` - deleted.**

Confirmed orphan. `0cab0ab spec: merge CR 'Reusable core - Phase 1...'` merged it,
then `688acde cleanup: complete reusable-core` deleted 6 work items and zero CR
files - the signature of the bug. Its work-items dir was already gone while the CR
file survived at `status: in-progress`.

No damage had occurred yet: exactly one `spec: merge` commit for reusable-core, and
the last reusable-core workitem commit predates the cleanup. The re-chunk/re-merge
loop had not fired. Specs were clean, so no spec repair was needed.

**2. `bff-canonical-brand-identifier.yaml` (shadow) - deleted; `03_` kept.**

Shadow was created by the chunk-time status save, not merge. Diff against the tracked
`03_bff-canonical-brand-identifier.yaml` showed **formatting only** - the shadow had
been re-indented by the YAML formatter on write, semantics identical.

Kept `03_` (preserves the ordering prefix and git history), carried the true status
across: `draft` -> `in-progress`, correct because chunking had started and
`.utopia/work-items/bff-canonical-brand-identifier/` exists.

Both changes left **uncommitted** in greenfield for review.

## Scan of every utopia-driven repo

Checked utopia, convoy, stdiz, domain, greenfield, email-updates-experiment,
ai-scaled. Only greenfield was affected - it is the only repo using `NN_` filename
prefixes, and the bug requires a prefix to trigger.

## Detection recipe, for reuse

Two cheap signals in `.utopia/change-requests/`:

- **Shadow pair:** both `NN_<id>.yaml` and `<id>.yaml` exist.
- **Orphan:** a CR with status other than `draft` whose `.utopia/work-items/<id>/`
  directory does not exist.

Before deleting an orphan, confirm it really merged (`git log --grep='spec: merge'`)
and check its `cleanup: complete` commit for zero CR deletions. Then check for
repeated `spec: merge` commits for the same CR - more than one means the re-run loop
fired and specs may carry duplicated criteria needing repair.
