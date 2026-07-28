# models config keys are validated but never read

Found while scoping the auth/credential CR (2026-07).

`ValidateModelConfig` (internal/domain/model.go:58-90) validates all 12 keys in
the `models:` section. But `ModelForCommand` (internal/domain/discovery.go:146-184)
has exactly **one** production caller: internal/validators/runner.go:96.

`config.Models` appears in production code in only two places:
- internal/store.go:282 - validation
- internal/ralph/ralph.go:67 - `WithModelConfig` on the validator runner

So `models.validators` is consumed, and `models.validator_router` is read
directly by `resolveRouterModel` (internal/validators/router.go:20-26). Every
other key is inert:

    models.cr, models.execute, models.harvest, models.discover,
    models.standards, models.refactor, models.shape,
    models.validator_create, models.validator_edit

Those commands only get a model from the `--model` flag via `ResolveModelFlag`
(internal/cli/root.go:111-126).

## Why this matters

This repo's own .utopia/config.yaml has:

    models:
      default: opus
      execute: opus     # <- no-op
      validators: opus  # <- actually works

So `execute: opus` reads as configuration and does nothing. Ralph runs on
whatever `claude` defaults to unless `--model` is passed.

Contradicts the model-configuration spec, feature `config-structure`:
"Per-command keys override the default (cr, harvest, execute, validators,
discover, standards, refactor, shape, validator_create, validator_edit)".

Looks like a bugfix CR against spec `model-configuration`, feature
`config-structure` - behaviour doesn't match what the spec already says.

Config that validates but does nothing is worse than no config, because it
reads as a working setting. Whatever fixes this should probably also cover
the general shape: a load-time validator with no runtime consumer.
