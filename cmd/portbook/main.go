package main

import (
	"context"
	"os"

	"github.com/sviatoslav/portbook/internal/cli"
	"golang.org/x/term"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, term.IsTerminal(int(os.Stdout.Fd()))))
}
