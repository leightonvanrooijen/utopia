package main

import (
	"github.com/leightonvanrooijen/utopia/internal/cli"
)

func main() {
	// Initialize execute command
	cli.InitExecuteCmd()

	// Run CLI
	cli.Execute()
}
