package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func getCommand(o *options, out io.Writer) *cobra.Command {
	var directory, owner, service string
	cmd := &cobra.Command{
		Use: "get [SERVICE]", Short: "Get an existing reserved port (default service: default)",
		Long:    "Read one registry snapshot without locks, writes, configuration reads, or TCP probes. A missing reservation fails. Success prints only the decimal port, even with --output json. The owner defaults to cwd, without Git root discovery.",
		Example: "  portbook get api\n  portbook get --directory /projects/my-app",
		Args:    serviceOwnerArgs(&directory, &owner, &service),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			c, err := state.Default()
			if err != nil {
				return &commandError{code: "storage_error", err: err}
			}
			port, err := (booking.Store{Config: config.Store{Coordinator: c}}).Get(owner, service)
			if err != nil {
				code := "storage_error"
				if errors.Is(err, booking.ErrNotFound) {
					code = "runtime_error"
				}
				return &commandError{code: code, err: err}
			}
			if _, err := fmt.Fprintf(out, "%d\n", port); err != nil {
				return &commandError{code: "runtime_error", err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&directory, "directory", "", "Existing absolute owner directory (default: current working directory)")
	once(cmd.Flags().Lookup("directory"))
	cmd.InitDefaultHelpFlag()
	return cmd
}
