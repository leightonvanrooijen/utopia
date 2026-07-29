# after-workitem validator feedback lost its dedicated stdout print

Found while investigating the verification `failure-stdout-logging` gap
(see CR `verification-fix-failure-stdout-logging`).

## What happened

Commit `0063841 feat: print validator failure feedback to stdout` added an explicit
print of validator feedback on the after-workitem path. The subscription/engine
refactor dropped it.

Today `internal/ralph/ralph.go:355-359` prints only:

    gate blocked workitem-verified, will retry with feedback

No payload. Compare the after-phase path at `internal/ralph/ralph.go:570-575`, which
still has its dedicated print:

    fmt.Printf("\n--- Validator Failure Feedback ---\n%s\n--- End Validator Feedback ---\n\n", feedback)

## Why it still appears to work

The feedback reaches the terminal by accident. It travels as `ConnectorResult.Stdout`:

- `internal/ralph/validators.go:132-135` sets `res.Stdout = o.agg.Feedback`
- `internal/ralph/engine.go:69-74` calls `logResolution(h)` on every joined handle
- `internal/ralph/engine.go:180-191` dumps captured output indented under the status line

So `agentic-validators` / `failure-stdout-logging` is currently satisfied only as a
side effect of the engine's resolution ledger, not by intent.

## Why it's worth fixing

Fragile. Any change to `logResolution`, to how handles carry output, or to whether
validators route through the engine silently breaks a specced behaviour with no test
covering it. The two validator paths also now format differently - one delimited
block, one indented ledger dump.

## Possible directions

- Restore the explicit print on the after-workitem path for symmetry, accepting a
  double-print unless `logResolution` is suppressed for that handle.
- Or go the other way: delete the after-phase explicit print and route after-phase
  validators through the engine too, so one mechanism serves all three (validators
  x2 + verification). This is the more interesting option - it would also make the
  verification fix unnecessary, since verification would inherit the print for free.

## Resolution

Now tracked. CR `unify-failure-output-printing` (initiative) takes the second option:
phase 1 is the narrow verification bugfix, phase 2 unifies all three paths onto one
mechanism and adds the missing regression tests.

Note kept for the archaeology - specifically that commit `0063841` is where the
after-workitem print came from, and the subscription/engine refactor is where it went.
