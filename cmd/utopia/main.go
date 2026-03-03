package main

import (
	"github.com/leightonvanrooijen/utopia/internal/cli"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	"github.com/leightonvanrooijen/utopia/internal/strategies/chunk/ralphsequential"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/leightonvanrooijen/utopia/internal/strategies/execute/sequential"
)

func main() {
	// Create strategy registries
	execRegistry := executeStrategy.NewRegistry()
	chunkRegistry := chunkStrategy.NewRegistry()

	// Register available strategies
	// Sequential strategy uses default dependencies created at execution time
	// with the correct projectDir from the Execute call
	execRegistry.Register(sequential.New())
	// SpecLoader is configured at runtime when chunking (via SetSpecLoader)
	chunkRegistry.Register(ralphsequential.New())

	// Initialize execute command with registries
	cli.InitExecuteCmd(execRegistry, chunkRegistry)

	// Run CLI
	cli.Execute()
}
