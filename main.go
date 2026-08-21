package main

import (
	"fmt"
	"os"

	"github.com/usenorn/runner/cmd"
	"github.com/usenorn/runner/internal/entity"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(entity.ExitCode(err))
	}
}
