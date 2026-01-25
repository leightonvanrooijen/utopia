package main

import (
	"github.com/leightonvanrooijen/utopia/internal/cli"
	"github.com/leightonvanrooijen/utopia/internal/strategies/chunk/ralphsequential"
	"github.com/leightonvanrooijen/utopia/internal/strategies/execute/sequential"
)

func main() {
	// Register available strategies
	cli.RegisterChunkStrategy(ralphsequential.New())
	cli.RegisterExecuteStrategy(sequential.New())

	// Run CLI
	cli.Execute()
}
