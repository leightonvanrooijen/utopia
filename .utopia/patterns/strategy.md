---
id: strategy
status: draft
---

# Strategy

## Description
A pluggable workflow implementation registered in a central registry, allowing different approaches to the same operation.

## Responsibility
- Define a consistent interface for a specific workflow type
- Encapsulate workflow-specific logic and user interaction
- Self-describe via Name() and Description() methods for CLI discoverability

## Boundaries
- Must not access other strategies directly
- Must not bypass the registry for strategy lookup
- Must not handle persistence (delegates to domain/infra layers)

## Naming
- Interface: `Strategy` in `internal/strategies/{workflow}/strategy.go`
- Implementation: `internal/strategies/{workflow}/{name}/strategy.go`
- Registry: `Registry` struct in the interface file

## Examples
- internal/strategies/spec/strategy.go
- internal/strategies/spec/guided/guided.go
- internal/strategies/execute/strategy.go
- internal/strategies/chunk/strategy.go
