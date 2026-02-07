---
id: strategy
status: approved
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

## How to Create a New Strategy

1. Create implementation file at `internal/strategies/{workflow}/{name}/strategy.go`
2. Implement the `Strategy` interface for that workflow type
3. Implement `Name()` returning a unique identifier (e.g., "sequential")
4. Implement `Description()` returning CLI help text
5. Register in `cmd/utopia/main.go`:

   ```go
   cli.Register{Workflow}Strategy({name}.New())
   ```

## Flow

```
main.go
   │
   ├── RegisterExecuteStrategy(sequential.New())
   ├── RegisterChunkStrategy(ralphsequential.New())
   │
   ▼
Registry.Register(strategy)
   │
   ▼
CLI command runs
   │
   ▼
Registry.Get(strategyName)
   │
   ▼
strategy.Execute(...) or strategy.Chunk(...)
```

## Examples

- internal/strategies/execute/strategy.go (interface)
- internal/strategies/execute/sequential/strategy.go (implementation)
- internal/strategies/chunk/strategy.go (interface)
- internal/strategies/chunk/ralphsequential/strategy.go (implementation)
