package main

import (
	"os"

	"github.com/nikitazigman/badger/internal/cli"
)

func main() {
	exitCode := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(int(exitCode))
}
