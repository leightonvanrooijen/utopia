# Hardcoded model IDs are several generations stale

Found while scoping the auth/credential CR (2026-07).

internal/domain/model.go:19-33:

    var modelMappings = map[ModelName]string{
        ModelHaiku:  "claude-3-5-haiku-20241022",
        ModelSonnet: "claude-sonnet-4-20250514",
        ModelOpus:   "claude-opus-4-20250514",
    }

Problems:

1. **claude-3-5-haiku-20241022 is retired** (Feb 2026). Requests using it get
   a 404, so `--model haiku` and `models.validator_router` (which defaults to
   haiku, internal/validators/router.go:15) are broken.
2. **claude-opus-4-20250514 and claude-sonnet-4-20250514 are deprecated**,
   retiring 2026-06-15 - i.e. already past. Also 404 candidates.
3. Date-suffixed IDs need a code change every model release. The bare aliases
   (`claude-opus-5`, `claude-sonnet-5`, `claude-haiku-4-5`) are stable strings
   that don't - the whole point of an alias.

The same spec that pins these is model-configuration, feature `model-mapping`,
whose acceptance criteria hardcode the stale IDs:

    - haiku maps to claude-3-5-haiku-20241022
    - sonnet maps to claude-sonnet-4-20250514
    - opus maps to claude-opus-4-20250514

So the spec has to change too, not just the code - the spec is asserting the
wrong thing. Probably an enhancement CR against `model-mapping`: map friendly
names to aliases rather than dated snapshots, so the mapping stops going stale.

## Related inconsistency

Two different value shapes reach `--model`:

- the flag path resolves to a **full ID** (ResolveModelFlag -> ResolveModel)
- the validator/router path passes a **short alias** (`opus`, `haiku`) straight
  through

Both happen to be accepted by the claude CLI, but `CLI.WithModel`'s doc comment
(internal/claude.go:70-75) claims full IDs only. Worth unifying on aliases.
