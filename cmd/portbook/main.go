package main

import (
	"context"
	"os"

	"golang.org/x/term"
	"portbook/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, term.IsTerminal(int(os.Stdout.Fd()))))
}
