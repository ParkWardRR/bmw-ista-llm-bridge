//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--demo" || arg == "demo" {
			runTUI()
			return
		}
	}
	fmt.Fprintln(os.Stderr, "This application requires Windows for full functionality.")
	fmt.Fprintln(os.Stderr, "Run with --demo for a preview TUI with mock data.")
	os.Exit(1)
}
