# Impl hints: commit/cleanup CRs by real filename (numeric prefix tolerance)

Relates to CR: `unified-change-request-system-commit-prefixed-cr-files`

## Root cause
A CR's physical filename can differ from its internal `id:` because files get a
numeric ordering prefix (`01_reusable-core.yaml` has `id: reusable-core`). Any
code that reconstructs the path as `<id>.yaml` misses the real file. Observed as:
`git add ... reusable-core.yaml -> fatal: pathspec did not match any files` (exit 128)
during the session-end auto-commit of `utopia cr` / `utopia refactor`.

## Sites to fix (all reconstruct `<crID>.yaml`)
- `internal/cli/execute.go` `GitCommitCR` (~line 657) - staging path for the create-commit.
- `internal/cli/execute.go` `gitCommitCleanup` (~line 651) - staging path for the cleanup-commit.
- `internal/store.go` `DeleteChangeRequest` (~line 377) - deletes `change-requests/<id>.yaml`.

## Suggested approach (reuse existing pattern)
Commit `14291ef` already added `store.ResolveChangeRequest` + `stripNumericPrefix`
in `internal/store.go` for the `utopia execute <name>` path. Mirror it:
- Add `store.ChangeRequestPath(id) (string, error)` that returns the actual file
  path: try canonical `<id>.yaml` first, else scan for a file whose basename with
  `stripNumericPrefix` applied equals `id`; ambiguous match -> error listing candidates.
- Have the commit/cleanup/delete sites resolve the real path via that method.
- Keep using the internal `id` for the commit **message** (`cr: create <id>`); only
  the staged/deleted **path** changes.
- The `cli -> internal` one-way import rule means the prefix helper stays in the store
  (same reason `ResolveChangeRequest` lives there, not in execute.go).

## Tests
- Prefixed file (`01_reusable-core.yaml`, id `reusable-core`) commits successfully
  with message `cr: create reusable-core`.
- Un-prefixed file still commits (canonical path wins).
- Ambiguous stripped-id (`ai-chat.yaml` + `06_ai-chat.yaml`) -> error, no commit.
- Post-merge cleanup removes the prefixed file and stages its removal.
