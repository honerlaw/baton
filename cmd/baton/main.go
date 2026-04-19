// Command baton runs a multi-stage agentic workflow against a codebase.
package main

import (
	"fmt"
	"os"

	"github.com/honerlaw/baton/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "baton: error: %s\n", err)
		os.Exit(1)
	}
}
