# Claude limit detection - research findings (2026-07)

Context: the ralph loop burned iterations retrying against the org monthly
spend limit message. Researched whether anything better than message parsing
exists before writing CR `execution-ralph-enhance-spend-limit-probing`.

## Findings (verified against official docs, July 2026)

- **Message parsing is the only universal detection mechanism.** No
  `claude usage` subcommand, no headless `/usage`, no JSON error output,
  no distinct exit codes (all limit errors are exit 1), no local state file
  under ~/.claude with reset times.
- **Known limit messages** (all free text on stderr):
  - `Limit reached · resets 1am (Australia/Sydney)` (legacy rolling)
  - `You've hit your session limit · resets 3:45pm` (current rolling, no tz)
  - `You've hit your Opus limit · resets 3:45pm` (per-model rolling)
  - `You've hit your org's monthly spend limit · run /usage-credits ...`
    (org cap - NO reset time, needs admin action or next billing cycle)
- **No API reports spend-cap state or reset time.** Spend Limits API is
  Enterprise-only, needs a scoped admin key, ~1hr delayed, and still omits
  reset times. Rate Limits API / Managed Agents headers cover per-minute
  rate limits only.
- **Agent SDK migration does not help with limits.** The SDK bundles the
  same Claude Code binary and draws from the same subscription limit pool
  (separate SDK credits were planned for June 2026 then paused). Its typed
  errors (`error_max_budget_usd`) cover only the SDK's own per-query budget,
  not the org cap. Official SDK auth for third-party tools is API-key only.

## Future improvement idea

If Anthropic ships a usage/limit-status API (open GitHub feature requests
exist: anthropics/claude-code#45392, #44328), replace the regex layer in
internal/ralph/ratelimit.go with state queries. Until then, probe-until-
recovery (per the CR) is the robust exit condition: detection gates entering
probe mode, but exiting it is empirical (a successful minimal invocation),
so future message rewording can't strand the loop.
