# Multi-provider agent CLI support - research findings (2026-07-29)

Scoping notes from investigating whether Utopia could drive Codex / Gemini as well
as Claude Code. No CR for this yet beyond 10_conversations-hook-reported-transcript-path.
Written down so the next attempt doesn't re-research it.

## The big finding: hooks are converging into a de facto standard

All three CLIs now ship a Claude-Code-shaped hook system: ~11 lifecycle events,
payload delivered as JSON on stdin, containing `session_id` + `transcript_path`
+ `cwd` + `hook_event_name`. Enabled by default on all three.

This means the "spawn the vendor's native TUI, then read back the transcript"
pattern is portable, and no provider requires reconstructing the transcript path
by mangling the cwd. That was the thing I assumed made interactive
multi-provider infeasible. It isn't.

Register `SessionStart` rather than `SessionEnd` where possible - same
`transcript_path`, but no teardown time budget and it survives the session being
killed.

## Injecting a hook without touching the user's config

This is where they differ, and it's the real cost.

- **Claude**: `--settings '{...}'` accepts inline JSON. Cleanest by far. Verified.
- **Gemini**: no inline flag. Use `GEMINI_CLI_SYSTEM_SETTINGS_PATH=/tmp/x.json`.
  Safe because every hook event array uses CONCAT merge, so user hooks survive.
  Also supports `--session-id <uuid>` to SET a new session id (verified: sets,
  does not merely resume; works interactively), so you get free correlation.
- **Codex**: `-c 'hooks.SessionEnd=[...]'` should work inline (the `-c` layer
  becomes a non-managed `HookSource::SessionFlags`) but this was NOT empirically
  verified. The proven path is a `hooks.json` in `CODEX_HOME` plus
  `--dangerously-bypass-hook-trust`. **Do not enable that flag by default** - it
  disables a security review step. Codex has no way to pre-assign a session id.

Both Codex and Gemini silently drop settings-defined hooks in untrusted folders.

## Parsing is now the hard part, not locating

Three unstable internal JSONL formats, all declared non-contractual by their
vendors. Gemini's is the worst by a distance: it is event-sourced, with
`{"$set":{...}}` and `{"$rewindTo":"id"}` mutation records, so a reader has to
replay deltas rather than map messages 1:1. See
`packages/core/src/services/chatRecordingTypes.ts` before writing any Go parser.

## Hazards that would bite a long-running orchestrator

- Gemini **deletes abandoned sessions on exit** if the human quits without a
  real prompt.
- Gemini's retention sweep runs at **startup of a later launch** and can delete a
  transcript Utopia hasn't read yet. Configurable via
  `general.sessionRetention.{enabled,maxAge,maxCount,minRetention}`.
- Gemini's `transcript_path` can be `''` (empty string, not null) when recording
  is disabled, e.g. on ENOSPC.
- Codex `SessionEnd` has a ~1s default timeout, 3s hard cap, because it runs
  inside app-server's 5s shutdown window. Write the path and return; never do
  work inside the hook.
- Codex rollout files are **lazily materialized** and relocated on archive, so
  newest-file globbing is unsafe. (`SessionEnd` explicitly flushes and
  materializes first, which is why the hook is the reliable route.)
- Codex `notify` / `legacy_notify` is a dead end: one event, payload as a
  positional argv arg, stdin is null, and **no transcript path**.

## Gemini project directory naming - do not try to compute it

`~/.gemini/tmp/<x>/` is no longer a hash. It's a registry-allocated slug:
`slugify(basename(cwd))` plus a `-N` collision counter allocated under a lockfile
in `~/.gemini/projects.json`. The counter depends on global collision history, so
**it is not a pure function of cwd and cannot be reimplemented**. Reverse-lookup
via `~/.gemini/projects.json` or by scanning `~/.gemini/tmp/*/.project_root`.

## Billing / ToS

- `claude -p`, `codex exec`, `gemini -p` all bill to the subscription, not the
  metered API. No API key needed. A stray `ANTHROPIC_API_KEY` in env silently
  wins for Claude and bills API - guard against that.
- Anthropic's ToS softened materially since Feb 2026. The old blanket ban on
  using OAuth credentials in other tools is **deleted**. Current line is drawn at
  offering claude.ai login to, or routing requests on behalf of, *other users*.
  Personal use of your own subscription through your own tool is fine.
- **Risk to watch**: a change effective 2026-06-15 would have moved `claude -p`
  and Agent SDK usage onto separately-metered "Agent SDK credits" (est. 15-30x
  cost delta). It was **paused**, not cancelled. Anthropic said it is "revising
  the plan" and will give advance notice.
- Discard any third-party README claiming that change is already in effect.

## Decisions reached

- **ACP is rejected.** Its premise is that the *client* renders the conversation,
  which means hand-rolling the TUI. That is the one thing we don't want. It also
  has no rate-limit signal in the protocol at all (explicitly deferred to a
  future RFD), and the Claude `_claude/usage` extension PR was closed unmerged.
  Go SDKs are either stale (`coder/acp-go-sdk`, ~7 schema versions behind) or
  unproven (`spachava753/acp-sdk`, single maintainer). ACP v2 draft landed
  2026-07-20 and guidance is to support v1 and v2 side by side.
- **Claude Agent SDK is rejected** for this codebase: it bills to the plan
  correctly, but there is no Go binding ("not yet available for Go"), so it would
  mean a Node sidecar. Its own transport is
  `claude --output-format stream-json --input-format stream-json`, which Go can
  speak directly, and `sdk.d.ts` in the npm package is the de facto schema for
  exactly that invocation.
- **Split by mode, not by provider.** Interactive commands (`cr`, `shape`) keep
  the vendor's native TUI. Headless paths (ralph execute, validators, harvest,
  discover, standards, refactor) are where structured output and any
  multi-provider work belong - and where the token spend actually is.

## Why headless stream-json is worth doing on its own

`claude -p --output-format stream-json --verbose` emits a first-class
`rate_limit_event` with `status` (allowed / allowed_warning / rejected),
`resetsAt` (unix seconds), `rateLimitType`, and `utilization`. That replaces the
console-prose regex in `internal/ralph/ratelimit.go` and its 10-minute default
guess with real data. It is **edge-triggered** ("emitted when rate limit status
changes"), so cache the last-seen value; do not poll for it.

Also yields `total_cost_usd` + `modelUsage` per run, which Utopia currently has
zero of. Caveats: `usage` undercounts with subagents (use `modelUsage`); the cost
figures are client-side estimates and explicitly not billing-grade; dedupe
parallel tool-call assistant messages by nested `message.id`.

Parsing traps: `type: "system"` is a namespace with ~19 subtypes, and
`system/init` is NOT reliably the first event (hook events can precede it, and a
second `init` fires on later turns). Don't test `apiKeySource === "oauth"` to
detect subscription - a Team-plan OAuth session returned `"none"`. Use
`claude auth status`, which prints JSON including `authMethod` and
`subscriptionType`.

Rate-limit signal by provider: Claude structured (above); Codex only via the
**experimental** `app-server` JSON-RPC (`account/rateLimits/read`) - the
`codex exec --json` stream carries token usage but never quota; Gemini
effectively nothing (it parses `QuotaFailure`/`RetryInfo` internally but drops
everything except a class name at the JSON boundary, so at best you get
`TerminalQuotaError` vs `RetryableQuotaError`).

## Pre-existing issues worth their own CR, unrelated to providers

- `internal/domain/model.go:19-23` has **stale** model IDs
  (`claude-3-5-haiku-20241022`, `claude-sonnet-4-20250514`,
  `claude-opus-4-20250514`).
- The `--model` value shape is inconsistent: `cli/root.go:116` resolves to a full
  dated ID, `validators/runner.go:107` passes a short alias. This works only
  because the Claude CLI accepts both, and becomes a real bug the moment a second
  provider exists.
