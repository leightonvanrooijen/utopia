# Batch ordering trap, and orphaned work-item dirs

Two things found while sequencing the CR queue for `utopia execute --all` (2026-07).

## 1. `--all` ordering read the wrong key - FIXED (CR `01_fix-batch-execution-filename-ordering`)

Spec `execution-ralph` / `batch-execution` says ordering is by **filename**
(`.utopia/specs/execution-ralph.yaml:161` and `:166`). The implementation sorted on
`cr.ID`:

- `lessCRExecutionOrder` (`internal/cli/execute.go:234`) calls `crNumericPrefix(a.ID)`
- `cr.ID` comes only from the yaml `id:` field - `Load[T]` (`internal/store.go:99`)
  never injects the filename
- `ListChangeRequests` (`internal/store.go:468`) computes the filename basename to
  load each file, then throws it away

The documented convention is prefix-on-filename, clean-id-inside - see the doc comment
on `ResolveChangeRequest` (`internal/store.go:337`): "carries its own internal id
(independent of any filename prefix)". Greenfield's real data matched
(`01_reusable-core.yaml` containing `id: reusable-core`).

Net effect: under the documented convention, **every CR looked unprefixed and the
batch ran in alphabetical id order.** The ordering prefix did nothing, silently.

Same root cause as the shadow-save bug fixed in `1ee0cdc` - both assumed `cr.ID`
and the filename agree. That fix corrected `SaveChangeRequest`; the ordering half
was missed.

### The fix

`ListChangeRequestFiles` (`internal/store.go`) now returns each CR paired with its
filename basename as `internal.ChangeRequestFile`; `ListChangeRequests` is a thin
wrapper over it for the callers that only want documents. `lessCRExecutionOrder` sorts
on `Basename`. `crNumericPrefix` is gone - the convention has one definition,
`internal.NumericFilenamePrefix`, which `stripNumericPrefix` also delegates to.

Only sequencing changed. Work item keying, progress output and commit messages still
key off `cr.ID`.

### Workaround no longer needed

The 8 queued CRs carry the prefix in **both** the filename and the `id:` field, which
ordered correctly under the old id-based sort *and* under the fixed filename-based one.
Prefix-in-filename alone is now sufficient; new CRs should keep the id clean
(`01_reusable-core.yaml` holding `id: reusable-core`).

### Gotcha for the batch that contains its own fix

`runExecuteAll` calls `ListChangeRequests` and `sort.Slice` **once, before the loop**
(`internal/cli/execute.go:251-259`), and the running binary is not rebuilt mid-run. So
CR `01_` cannot reorder the batch it is part of. It pays off from the next run onward.
This is why the id prefixes were needed, not just filenames.

## 2. The two batch paths disagree

Still open after the `01_` fix, which deliberately touched only `--all`:

| | ordering | failure policy |
|---|---|---|
| `execute --all` | numeric **filename** prefix, sorted | fail-fast, aborts batch |
| `execute run` daemon (`execute_run.go:128`) | none - raw `os.ReadDir` order | resilient, logs and skips |

So `10_x` runs *before* `2_x` in the daemon (`os.ReadDir` sorts filenames as strings, so
`"10_" < "2_"`), and the daemon only picks up CRs with status `approved` while `--all`
ignores status entirely. The daemon calls `ListChangeRequests`, so adopting the fixed
ordering is now a two-line change - switch it to `ListChangeRequestFiles` and
`sort.Slice(…, lessCRExecutionOrder)`. Deliberately left alone: whether the divergence
is intentional (interactive batch vs unattended queue) is an ADR-shaped question, not a
bugfix. Note the failure policies would still differ.

## 3. Orphaned work-item dirs in utopia

`.utopia/work-items/` holds 8 directories with **no corresponding CR file**:
`change-request-system`, `chunking-ralph-sequential`, `cleanup-legacy-system`,
`cli-extract-services-layer`, `refactoring`, `unified-change-request-system`,
`verification`, `yaml-formatting`.

They are harmless for execution - `--all` iterates CR *files*, so nothing re-runs. But
they inflate `utopia status`: it reports 54 work items, 16 pending and 1 in-progress,
essentially all from dead CRs. The single `in_progress` is
`cli-extract-services-layer-extract-merge-service`.

This is the mirror image of the orphan case in
`greenfield-orphaned-cr-files-cleanup.md`: there, CR files survived without work
items; here, work items survive without CR files. Cleanup is a judgement call -
confirm each merged (`git log --grep='spec: merge'`) before deleting.

Note: the dirs for `02_chunking-...-document` and `03_harvest-execution-runs` were
deliberately deleted when those CRs were re-prefixed, so they re-chunk from the CR
rather than stranding work items keyed to the old id.
