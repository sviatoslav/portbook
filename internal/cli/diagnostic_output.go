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
	if cmd == nil || cmd.Flag("output") == nil {
		return fallback
	}
	start, misplaced := outputScopeStart(args, cmd.Root())
	if misplaced || start < 0 {
		return fallback
	}
	args = args[start:]
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

// outputScopeStart walks command groups until a leaf defines its own output
// flag. Prefix flags cannot borrow a descendant's output flag through Cobra's
// command search. Known flag values are consumed, including output-shaped paths.
// Cobra still owns operand/flag validation and command dispatch.
func outputScopeStart(args []string, root *cobra.Command) (start int, misplaced bool) {
	// Find does not parse flags or execute handlers. Use its resolved command's
	// metadata even before the command path: Cobra accepts leaf flags there,
	// and their values must not hide a later, misplaced --output.
	resolved, _, _ := root.Find(args)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if name, long := strings.CutPrefix(arg, "--"); long {
			name, _, assigned := strings.Cut(name, "=")
			if name == "output" {
				misplaced = true
			}
			flag := resolved.Flag(name)
			if !assigned && ((flag != nil && flag.NoOptDefVal == "") || name == "output") {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		found := false
		for _, child := range root.Commands() {
			if child.Name() == arg {
				found = true
				if child.Flags().Lookup("output") != nil {
					return i + 1, misplaced
				}
				root = child
				break
			}
		}
		if !found {
			break
		}
	}
	return -1, misplaced
}
