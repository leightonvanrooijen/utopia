package main

import (
	"github.com/leightonvanrooijen/utopia/internal/cli"
	"github.com/leightonvanrooijen/utopia/internal/strategies/chunk/ralphsequential"
	"github.com/leightonvanrooijen/utopia/internal/strategies/execute/sequential"
)

func main() {
	// Register available strategies
	// Sequential strategy uses default dependencies created at execution time
	// with the correct projectDir from the Execute call
	cli.RegisterExecuteStrategy(sequential.New())
	// SpecLoader is configured at runtime when chunking (via SetSpecLoader)
	cli.RegisterExecuteChunkStrategy(ralphsequential.New())

	// Run CLI
	cli.Execute()
}
