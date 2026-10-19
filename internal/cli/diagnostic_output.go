package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// diagnosticOutput recovers only the output selection: Cobra can stop parsing
// before reaching it on usage errors. It does not resolve commands, validate
// flags, or affect execution; Cobra remains authoritative for all of those.
// Stop at -- and retain the first selection, matching onceValue on duplicates.
// An invalid or missing first value falls back to the terminal default.
func diagnosticOutput(args []string, fallback string, cmd *cobra.Command) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// A known value-taking flag consumes its next token even if that token looks
		// like --output. Use Cobra's resolved command and flag metadata so local
		// flags do not consume diagnostic selections on unrelated commands.
		if name, long := strings.CutPrefix(arg, "--"); long && name != "output" {
			flag := cmd.Flag(name)
			if flag != nil && flag.NoOptDefVal == "" {
				if i+1 < len(args) && args[i+1] == "--" {
					return fallback
				}
				i++
				continue
			}
		}
		if arg == "--" {
			break
		}
		value, found := strings.CutPrefix(arg, "--output=")
		if arg == "--output" {
			if i+1 == len(args) {
				return fallback
			}
			value, found = args[i+1], true
		}
		if found {
			if value == "table" || value == "json" {
				return value
			}
			return fallback
		}
	}
	return fallback
}
