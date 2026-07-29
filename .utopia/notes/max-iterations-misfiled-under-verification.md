# `max_iterations` lives under `verification`, but it is a loop bound

Found while designing CR `10_work-item-turn-budget-and-sizing`.

`.utopia/config.yaml`:

```yaml
verification:
    command: ./scripts/verify.sh
    max_iterations: 6
```

`max_iterations` bounds how many times the **Ralph loop** retries a work item. It
is not a property of verification - verification is a command that runs once per
iteration and returns pass/fail. The loop is what iterates.

Evidence it is misfiled: the `verification` spec owns the field
(`verification-config`: "Config has optional max_iterations field (no limit if
not set)") while `execution-ralph`'s `ralph-loop` is the feature that actually
consumes it ("Loop continues until success or max iterations (if configured)").
The spec that documents the field and the spec that uses it are different specs.

## Why it surfaced

CR 10 adds `work_items.turn_budget` - a per-iteration turn ceiling. That knob and
`max_iterations` are siblings: together they define the cost ceiling for a work
item (`turn_budget x max_iterations`). CR 10 deliberately files the new knob under
a new `work_items` section rather than joining `max_iterations` under
`verification`, so the two related bounds now sit in different places.

That split is the lesser evil - better than filing a second knob in the wrong
drawer - but it leaves the config incoherent until `max_iterations` moves.

## Options

- Move to `work_items.max_iterations`, next to `turn_budget`. Both bounds are
  properties of executing a work item. Needs a deprecation path for existing
  configs (the loader already silently ignores unknown fields, so a read-both /
  write-new shim is cheap).
- Move to a new `ralph:` or `execution:` section. Groups by the component that
  enforces, not the thing bounded. Splits it from `turn_budget`, which the
  chunker also reads - so this reintroduces the problem CR 10 avoided.
- Leave it. Costs nothing today; costs a little confusion every time someone
  reasons about run cost and has to look in two places.

Not filed as a CR - it is a config-ergonomics cleanup with a migration concern,
worth doing deliberately rather than bundled into CR 10.
