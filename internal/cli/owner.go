package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
)

func serviceOwnerArgs(directory, owner, service *string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		help, _ := cmd.Flags().GetBool("help")
		if len(args) != 1 && !(help && len(args) == 0) {
			return fmt.Errorf("%s requires exactly one service", cmd.Name())
		}
		if len(args) == 1 {
			*service = args[0]
			if err := booking.ValidateService(*service); err != nil {
				return err
			}
		}

		selected := *directory
		if !cmd.Flags().Changed("directory") {
			var err error
			selected, err = os.Getwd()
			if err != nil {
				return &commandError{code: "runtime_error", err: err}
			}
		}
		var err error
		*owner, err = booking.Directory(selected)
		return err
	}
}
