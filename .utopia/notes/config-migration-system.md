# Config Migration System

## The Problem
When new config fields are added, existing projects have stale config files that don't include the new fields. Currently we apply defaults at runtime, but this means:
- Users don't see new options in their config file
- Config files don't self-document available options
- Harder to discover what's configurable

## Idea: Maintainable Config Migrations
A system that can update config files when new fields are added, similar to database migrations.

### Possible Approaches

**1. Version-based migrations**
- Config file has a `version` field
- Each version bump has a migration function
- `utopia init` or `utopia upgrade` runs pending migrations

**2. Schema diff approach**
- Compare loaded config struct against file on disk
- Detect missing fields
- Prompt user or auto-add with defaults + comments

**3. Init --upgrade flag**
- `utopia init` is already re-runnable
- Add `--upgrade` that preserves existing values but adds new fields

### Considerations
- Should preserve user comments in YAML (hard with standard yaml libs)
- Should not overwrite user customizations
- Should be idempotent (safe to run multiple times)
- Should work incrementally (not require running from scratch)

## Triggered By
Discussion during CR creation for validator_concurrency config option (2024-03).
