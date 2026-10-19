// Package cli defines the command tree and translates application results to output.
package cli

import (
	"context"
	"errors"
	"io"
	"os"
)

type commandError struct {
	code string
	err  error
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

// Run keeps terminal detection at the executable boundary. Cobra's automatic
// diagnostics are silenced so every command shares our output/exit contract.
func Run(ctx context.Context, args []string, out, diagnostics io.Writer, terminal bool) int {
	return run(ctx, args, out, diagnostics, terminal, defaultSkillInstaller(os.Stdin, out, diagnostics))
}

func run(ctx context.Context, args []string, out, diagnostics io.Writer, terminal bool, installer skillInstaller) int {
	output := "json"
	if terminal {
		output = "table"
	}
	options := &options{output: output, defaultOutput: output, installer: installer}
	var helpErr error
	root := newCommandTree(options, out, &helpErr)
	root.SetArgs(append([]string{}, args...))
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		err = helpErr
	}
	if err == nil {
		return 0
	}
	code, status := "usage_error", 2
	var classified *commandError
	if errors.As(err, &classified) {
		code, status = classified.code, 1
	}
	return fail(diagnostics, diagnosticOutput(args, output, cmd), code, err, status)
}
