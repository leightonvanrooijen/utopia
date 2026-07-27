# Migrate validators onto the connectors ecosystem

**Status:** parked until `connectors-plugin-system` CR is merged and proven.

## Context

The `connectors-plugin-system` CR (initiative, draft) introduces:

- Phase 1: ralph loop emits 8 lifecycle events via internal dispatcher
  (execution-started, workitem-started, workitem-verified, workitem-committed,
  phase-verified, phase-completed, execution-completed, execution-failed)
- Phase 2: new `connectors` spec - config-driven external commands subscribing
  to events. JSON payload on stdin, any language. Two modes: `gating`
  (git-hook semantics: exit 0 proceed, non-zero blocks - abort on pre-events,
  feedback-inject on workitem-verified/phase-verified) and `notify`
  (fire-and-forget, failures logged only).

First use cases: branch creation (gating on execution-started), PR creation
via gh (notify on execution-completed), agentic reviews (gating on
workitem-verified).

## The follow-up idea

Validators are conceptually gating connectors whose command is "run Claude
with this prompt file". Migrate them onto the dispatcher via an internal
adapter - user-facing validator config, `utopia validator` commands, and
output stay identical, so this is a refactor CR.

Mapping:
- `run: after-workitem` -> `on: [workitem-verified]`
- `run: after-phase` -> `on: [phase-verified]`
- `on-demand` -> not subscribed to any loop event (fixes the "trigger that
  means no trigger" wart)
- feedback injection -> gating mode's non-zero-exit stdout behavior

## The complication: early-start

Validators today start speculatively in parallel with verification (after
`<COMPLETE>` token, cancelled if verification fails) to hide Claude latency -
see `runVerificationWithValidators` in internal/ralph/ralph.go. Naive
migration to workitem-verified would serialize them after verification and
slow every iteration.

Options:
1. Accept the latency - simplest, real regression per work item
2. Speculative dispatch - dispatcher supports "start subscribers at event A,
   collect at gate B, cancel if B never fires". Generalizes early-start for
   any slow gating connector; meaningful design work
3. Special-case early-start - validators register via dispatcher but keep
   the custom start-together choreography. Pragmatic middle ground

## Resolution direction (decided in follow-up discussion)

Option 2, expressed as event vocabulary + process signals rather than
dispatcher magic. Two additions to the connectors spec (enhancement CR,
since connectors-plugin-system is already in-progress):

- New event `workitem-completion-claimed`: fires when the <COMPLETE> token
  is found, before verification runs (the moment validators start today)
- New event `workitem-verification-failed`: fires when verification fails;
  the runner SIGTERMs (then SIGKILLs) in-flight speculative connectors.
  Also useful for failure-alerting/metrics connectors
- Generalize connector config to launch/join/abort (rejected `early_start`
  flag as too hardcoded to the validator use case):
  - `on: <event>` - launch: when the process starts
  - `await: <event>` (optional) - join: loop blocks at this event on the
    connector's exit code. Present = gating, absent = notify - the mode
    enum dissolves. Synchronous gate = await equal to on
  - `cancel_on: [<events>]` (optional) - abort: kill the in-flight run
  - Config validation enforces loop-order invariants: await same-or-later
    than on; cancel_on events fall between on and await

Validator wiring becomes:
  on: workitem-completion-claimed
  await: workitem-verified
  cancel_on: [workitem-verification-failed]

Flow: token found -> speculative connector starts in parallel with
verification -> verification fails: kill connector, iterate | verification
passes: await connector exit, gate on exit code.

Note: the in-progress connectors-plugin-system CR keeps mode gating|notify;
those are the degenerate cases of this model. The migration CR evolves the
schema (mode -> await/cancel_on) - decide backward compatibility then.

Constraint to document: early-start connectors must be side-effect-safe
before exit (a killed run must leak nothing external). Read-only reviewers
are fine; anything posting comments/notifications must not do so mid-run.

With this, validators need zero privileged loop access - the
runVerificationWithValidators choreography becomes generic dispatcher
behavior and validators become pure config over connectors.

## Scope notes

- Touches `agentic-validators` spec (validator-execution-hook,
  validator-run-triggers) and the `connectors` spec
- Separate CR, not a phase 3 on connectors-plugin-system - depends on
  connectors being merged first
- Leave verification native - it decides whether an iteration succeeded at
  all; the loop needs one built-in notion of "done"
