package main

import (
	"fmt"
	"os"

	"github.com/nikitazigman/badger/internal/tui"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: badger <file.db>")
		os.Exit(2)
	}

	path := os.Args[1]
	if err := tui.Run(path, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open TUI for %q: %v\n", path, err)
		os.Exit(1)
	}
}
