# Unified System Cleanup

This note captures the cleanup tasks to be performed using the new unified CR system
once it's implemented. This serves as both a todo list and a test of the new system.

## Specs to Delete

These specs are replaced by `unified-change-request-system.yaml`:

- [ ] `.utopia/specs/spec-creation.yaml`
- [ ] `.utopia/specs/change-request-system.yaml`
- [x] `.utopia/specs/refactoring.yaml`

## Code to Remove

Once the unified system is in place, remove legacy code:

- [ ] `internal/domain/refactor.go` - Refactor domain type (becomes CR with type: refactor)
- [ ] `internal/cli/refactor.go` - Separate refactor command (unified into CR flow)
- [ ] `internal/cli/spec.go` - Separate spec command (unified into CR flow)
- [ ] Storage methods for separate refactor handling
- [ ] `LoadSpecOrChangeRequestOrRefactor()` - simplify to just `LoadChangeRequest()`
- [ ] `ToSpec()` conversion methods - no longer needed

## Directory Structure Changes

- [ ] Move `.utopia/specs/_changerequests/` to `.utopia/change-requests/`
- [ ] Remove `.utopia/refactors/` directory
- [ ] Remove `.utopia/specs/_drafts/` directory (CRs go directly to change-requests/)

## Commands to Update

- [ ] `utopia spec` → `utopia cr` (or keep as alias?)
- [ ] `utopia refactor` → removed (use `utopia cr` with type: refactor)
- [ ] `utopia chunk` → only loads from change-requests/
- [ ] `utopia execute` → terminology update (spec-id → cr-id)
- [ ] `utopia merge` → handles multi-spec targeting

## Test: First CR Using New System

Create this CR using the new unified system to validate it works:

```yaml
id: cleanup-legacy-system
type: removal
title: Remove Legacy Spec/Refactor System
tasks:
  - id: delete-old-specs
    description: Delete the three replaced spec files
    acceptance_criteria:
      - spec-creation.yaml is deleted
      - change-request-system.yaml is deleted
      - refactoring.yaml is deleted

  - id: remove-refactor-domain
    description: Remove the separate Refactor domain type
    acceptance_criteria:
      - internal/domain/refactor.go is deleted
      - internal/domain/refactor_test.go is deleted
      - All references to domain.Refactor are removed

  - id: simplify-storage
    description: Remove polymorphic loading, just use CRs
    acceptance_criteria:
      - LoadSpecOrChangeRequestOrRefactor removed
      - LoadChangeRequest is the only entry point
      - ToSpec() methods removed
```

---

Created: During unified-change-request-system design session
Purpose: Test the new system by using it to clean up after itself
