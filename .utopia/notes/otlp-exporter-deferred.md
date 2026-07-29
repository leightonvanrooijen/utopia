# OTLP exporter: deliberately deferred

Decided while scoping `observability-execution-spans` (2026-07).

## The decision

Adopt the OpenTelemetry **data model** (spans, parent/child, attributes, semantic
conventions). Do **not** adopt the OpenTelemetry **transport** (OTLP, collector,
exporter lifecycle, sampling) yet.

Spans are collected in-process and serialised into the run record at
`.utopia/runs/<cr-id>/<workitem-id>.yaml`, which is committed to git.

## Why

Utopia is a CLI that lives a few minutes and exits. OTel's operational surface
assumes a long-running process shipping to a collector:

- needs a collector or vendor endpoint to point at, or you get nothing back
- needs force-flush before process exit, or the last spans vanish
- needs sampling and retention decisions
- needs a backend to actually look at the data

Meanwhile git already gives us the time dimension for free. Run records are
committed, so `git log` plus a `yq` query answers "is Claude time getting worse
across runs" without any of the above.

Cost split when scoping this: the data model is roughly 10% of the work and most
of the value. The transport is most of the remaining cost and, without a backend,
none of the value.

## Why it stays cheap to add later

Because phase 1 of `observability-execution-spans` registers the in-process
collector as a real `sdktrace.SpanProcessor` against the real OTel SDK, adding
OTLP later is an **exporter registration**, not a re-modelling. No call site
changes. Roughly one file plus config.

## Revisit when

- There's an actual collector or vendor (Honeycomb / Grafana / Datadog) to receive it
- Utopia grows a long-running mode where flush-on-exit stops being awkward
  (the `queue-watch-daemon` feature is the likely trigger)
- Multiple machines run Utopia and runs need aggregating outside one git repo

## Related

- `observability-execution-spans` - phase 1 adopts the model
- `observability-unified-output` - creates the `observability` spec and the diagnostic channel
- `harvest-execution-runs` - creates the run record that spans get persisted into
