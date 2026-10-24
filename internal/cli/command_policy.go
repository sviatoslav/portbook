package cli

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/config"
)

type helpPolicy int

const (
	helpIndependent     helpPolicy = iota // Only explicit flags and operands are validated.
	helpConfigSelection                   // Also resolve the effective configuration location.
	helpConfigRepair                      // Validate overrides without reading the saved pointer.
)

type commandPolicy struct {
	help             helpPolicy
	structuredOutput bool
}

// command keeps help validation and local output flags beside each definition.
// Execution handlers still resolve mutation inputs inside their own state lock.
func (o *options) command(cmd *cobra.Command, policy commandPolicy) *cobra.Command {
	cmd.InitDefaultHelpFlag()
	if policy.structuredOutput {
		cmd.Flags().StringVar(&o.output, "output", o.output, "Output format: table or json (default: table on a terminal, json otherwise)")
		once(cmd.Flags().Lookup("output"))
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		o.helpErr = o.renderHelp(cmd, policy.help)
	})
	return cmd
}

func (o *options) renderHelp(cmd *cobra.Command, policy helpPolicy) error {
	// Cobra skips Args for help. Validate supplied operands while allowing
	// missing required operands, as defined by each command's Args handler.
	if err := o.validate(); err != nil {
		return err
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		return err
	}
	if policy != helpIndependent {
		location, err := config.DefaultLocation(o.configDir, o.configDirSet())
		if err == nil && policy == helpConfigSelection {
			_, err = location.Resolve()
		}
		if err != nil {
			return &commandError{code: "storage_error", err: err}
		}
	}
	text := cmd.Long
	if text == "" {
		text = cmd.Short
	}
	text += "\n\n" + cmd.UsageString()
	var err error
	if o.format() == "json" {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Command string `json:"command"`
			Help    string `json:"help"`
		}{"help", text})
	} else {
		_, err = io.WriteString(cmd.OutOrStdout(), text)
	}
	if err != nil {
		return &commandError{code: "runtime_error", err: err}
	}
	return nil
}
